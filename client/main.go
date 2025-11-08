package main

import (
    "context"
    "flag"
    "log"
    "time"

    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
    pb "github.com/mehdino123/grpc-k8s-deployer/proto"
)

func main() {
    serverAddress := flag.String("address", "grpc-deployer-service.default.svc.cluster.local:50051", "The server address in the format host:port")
    namespace := flag.String("namespace", "default", "Namespace to deploy to")
    
    // Deployment options
    imageName := flag.String("image", "mehdino123/frontino-ossinov55", "Docker image name")
    appName := flag.String("app", "frontino-app", "Application name")
    replicas := flag.Int("replicas", 2, "Number of replicas")
    
    // Action flags
    deploy := flag.Bool("deploy", false, "Deploy an application")
    status := flag.Bool("status", false, "Get deployment status")
    list := flag.Bool("list", false, "List deployments")
    deploymentName := flag.String("deployment-name", "", "Name of the deployment to get status for")
    
    flag.Parse()

    // Set up a connection to the server.
    conn, err := grpc.Dial(*serverAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
    if err != nil {
        log.Fatalf("did not connect: %v", err)
    }
    defer conn.Close()
    c := pb.NewDeploymentServiceClient(conn)

    // Contact the server and print out its response.
    ctx, cancel := context.WithTimeout(context.Background(), time.Second)
    defer cancel()

    if *deploy {
        // Create a deployment request
        req := &pb.DeployRequest{
            Namespace: *namespace,
            ImageName: *imageName,
            AppName:   *appName,
            Replicas:  int32(*replicas),
            Labels: map[string]string{
                "app": *appName,
            },
            Ports: []*pb.ContainerPort{
                {
                    Name:          "http",
                    ContainerPort: 5173,
                    Protocol:      "TCP",
                },
            },
            ServiceConfig: &pb.ServiceConfig{
                ServiceType: "ClusterIP",
                Ports: []*pb.ServicePort{
                    {
                        Name:       "http",
                        Port:       80,
                        TargetPort: 5173,
                        Protocol:   "TCP",
                    },
                },
            },
        }

        r, err := c.DeployApp(ctx, req)
        if err != nil {
            log.Fatalf("could not deploy app: %v", err)
        }
        log.Printf("Deployment result: %s", r.GetMessage())
        log.Printf("Deployment name: %s", r.GetDeploymentName())
        log.Printf("Service name: %s", r.GetServiceName())
    } else if *status {
        if *deploymentName == "" {
            log.Fatal("deployment-name is required when getting status")
        }
        
        req := &pb.StatusRequest{
            Namespace:      *namespace,
            DeploymentName: *deploymentName,
        }

        r, err := c.GetDeploymentStatus(ctx, req)
        if err != nil {
            log.Fatalf("could not get deployment status: %v", err)
        }
        log.Printf("Status result: %s", r.GetMessage())
        if r.GetDeploymentInfo() != nil {
            info := r.GetDeploymentInfo()
            log.Printf("Deployment: %s/%s", info.GetNamespace(), info.GetName())
            log.Printf("Replicas: %d/%d ready", info.GetReadyReplicas(), info.GetReplicas())
            log.Printf("Available: %d", info.GetAvailableReplicas())
            log.Printf("Conditions:")
            for _, condition := range info.GetConditions() {
                log.Printf("  - %s", condition)
            }
        }
    } else if *list {
        req := &pb.ListRequest{
            Namespace: *namespace,
        }

        r, err := c.ListDeployments(ctx, req)
        if err != nil {
            log.Fatalf("could not list deployments: %v", err)
        }
        log.Printf("List result: %s", r.GetMessage())
        log.Printf("Found %d deployments:", len(r.GetDeployments()))
        for _, deployment := range r.GetDeployments() {
            log.Printf("  - %s/%s: %d/%d ready", deployment.GetNamespace(), deployment.GetName(), 
                deployment.GetReadyReplicas(), deployment.GetReplicas())
        }
    } else {
        log.Fatal("Please specify an action: --deploy, --status, or --list")
    }
}