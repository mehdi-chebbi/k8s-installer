package main

import (
    "context"
    "flag"
    "fmt"
    "log"
    "net"

    appsv1 "k8s.io/api/apps/v1"
    corev1 "k8s.io/api/core/v1"
    "k8s.io/apimachinery/pkg/api/errors"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/util/intstr"
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/rest"

    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
    pb "github.com/mehdino123/grpc-k8s-deployer/proto"
)

// server implements the DeploymentServiceServer interface
type server struct {
    pb.UnimplementedDeploymentServiceServer
    clientset *kubernetes.Clientset
}

// DeployApp implements the DeploymentServiceServer interface
func (s *server) DeployApp(ctx context.Context, req *pb.DeployRequest) (*pb.DeployResponse, error) {
    namespace := req.GetNamespace()
    if namespace == "" {
        namespace = "default"
    }

    imageName := req.GetImageName()
    if imageName == "" {
        return nil, status.Error(codes.InvalidArgument, "image_name is required")
    }

    appName := req.GetAppName()
    if appName == "" {
        appName = "frontino-app"
    }

    replicas := req.GetReplicas()
    if replicas == 0 {
        replicas = 2
    }

    log.Printf("Received deployment request for image %s in namespace %s", imageName, namespace)

    // Deploy the application
    deploymentName, serviceName, err := deployApp(s.clientset, namespace, imageName, appName, replicas, req.GetLabels(), req.GetAnnotations(), req.GetPorts(), req.GetServiceConfig())
    if err != nil {
        log.Printf("Error deploying application: %v", err)
        return nil, status.Errorf(codes.Internal, "Failed to deploy application: %v", err)
    }

    return &pb.DeployResponse{
        Success:        true,
        Message:        fmt.Sprintf("Successfully deployed %s to namespace %s", imageName, namespace),
        DeploymentName: deploymentName,
        ServiceName:    serviceName,
    }, nil
}

// GetDeploymentStatus implements the DeploymentServiceServer interface
func (s *server) GetDeploymentStatus(ctx context.Context, req *pb.StatusRequest) (*pb.StatusResponse, error) {
    namespace := req.GetNamespace()
    if namespace == "" {
        namespace = "default"
    }

    deploymentName := req.GetDeploymentName()
    if deploymentName == "" {
        return nil, status.Error(codes.InvalidArgument, "deployment_name is required")
    }

    log.Printf("Received status request for deployment %s in namespace %s", deploymentName, namespace)

    deployment, err := s.clientset.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
    if err != nil {
        if errors.IsNotFound(err) {
            return nil, status.Errorf(codes.NotFound, "Deployment %s not found in namespace %s", deploymentName, namespace)
        }
        return nil, status.Errorf(codes.Internal, "Failed to get deployment: %v", err)
    }

    // ReadyReplicas and AvailableReplicas are now direct values, not pointers
    readyReplicas := deployment.Status.ReadyReplicas
    availableReplicas := deployment.Status.AvailableReplicas

    var conditions []string
    for _, condition := range deployment.Status.Conditions {
        conditions = append(conditions, fmt.Sprintf("%s: %s", condition.Type, condition.Status))
    }

    return &pb.StatusResponse{
        Success: true,
        Message: fmt.Sprintf("Retrieved status for deployment %s", deploymentName),
        DeploymentInfo: &pb.DeploymentInfo{
            Name:              deployment.Name,
            Namespace:         deployment.Namespace,
            Replicas:          *deployment.Spec.Replicas,
            ReadyReplicas:     readyReplicas,
            AvailableReplicas: availableReplicas,
            Conditions:        conditions,
        },
    }, nil
}

// ListDeployments implements the DeploymentServiceServer interface
func (s *server) ListDeployments(ctx context.Context, req *pb.ListRequest) (*pb.ListResponse, error) {
    namespace := req.GetNamespace()
    if namespace == "" {
        namespace = "default"
    }

    log.Printf("Received list request for deployments in namespace %s", namespace)

    listOptions := metav1.ListOptions{}
    if req.GetLabelSelector() != nil {
        var selector string
        for key, value := range req.GetLabelSelector() {
            if selector != "" {
                selector += ","
            }
            selector += fmt.Sprintf("%s=%s", key, value)
        }
        listOptions.LabelSelector = selector
    }

    deployments, err := s.clientset.AppsV1().Deployments(namespace).List(ctx, listOptions)
    if err != nil {
        return nil, status.Errorf(codes.Internal, "Failed to list deployments: %v", err)
    }

    var deploymentInfos []*pb.DeploymentInfo
    for _, deployment := range deployments.Items {
        // ReadyReplicas and AvailableReplicas are now direct values, not pointers
        readyReplicas := deployment.Status.ReadyReplicas
        availableReplicas := deployment.Status.AvailableReplicas

        var conditions []string
        for _, condition := range deployment.Status.Conditions {
            conditions = append(conditions, fmt.Sprintf("%s: %s", condition.Type, condition.Status))
        }

        deploymentInfos = append(deploymentInfos, &pb.DeploymentInfo{
            Name:              deployment.Name,
            Namespace:         deployment.Namespace,
            Replicas:          *deployment.Spec.Replicas,
            ReadyReplicas:     readyReplicas,
            AvailableReplicas: availableReplicas,
            Conditions:        conditions,
        })
    }

    return &pb.ListResponse{
        Success:     true,
        Message:     fmt.Sprintf("Retrieved %d deployments", len(deploymentInfos)),
        Deployments: deploymentInfos,
    }, nil
}

func main() {
    var port int

    // Parse command line flags
    flag.IntVar(&port, "port", 50051, "Port to listen on")
    flag.Parse()

    // Use in-cluster configuration when running inside a pod
    config, err := rest.InClusterConfig()
    if err != nil {
        log.Fatalf("Error getting in-cluster config: %v", err)
    }
    log.Println("Using in-cluster configuration")

    // Create the Kubernetes clientset
    clientset, err := kubernetes.NewForConfig(config)
    if err != nil {
        log.Fatalf("Error creating Kubernetes client: %v", err)
    }

    log.Printf("Successfully connected to Kubernetes cluster")

    // Set up gRPC server
    lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
    if err != nil {
        log.Fatalf("failed to listen: %v", err)
    }
    s := grpc.NewServer()
    pb.RegisterDeploymentServiceServer(s, &server{clientset: clientset})
    log.Printf("gRPC server listening at %v", lis.Addr())
    if err := s.Serve(lis); err != nil {
        log.Fatalf("failed to serve: %v", err)
    }
}

func deployApp(clientset *kubernetes.Clientset, namespace, imageName, appName string, replicas int32, labels, annotations map[string]string, ports []*pb.ContainerPort, serviceConfig *pb.ServiceConfig) (string, string, error) {
    // Default labels if none provided
    if labels == nil {
        labels = map[string]string{
            "app": appName,
        }
    }

    deploymentName := fmt.Sprintf("%s-deployment", appName)
    serviceName := fmt.Sprintf("%s-service", appName)

    // Create a Deployment
    deployment := &appsv1.Deployment{
        ObjectMeta: metav1.ObjectMeta{
            Name:        deploymentName,
            Namespace:   namespace,
            Labels:      labels,
            Annotations: annotations,
        },
        Spec: appsv1.DeploymentSpec{
            Replicas: &replicas,
            Selector: &metav1.LabelSelector{
                MatchLabels: labels,
            },
            Template: corev1.PodTemplateSpec{
                ObjectMeta: metav1.ObjectMeta{
                    Labels:      labels,
                    Annotations: annotations,
                },
                Spec: corev1.PodSpec{
                    Containers: []corev1.Container{
                        {
                            Name:  appName,
                            Image: imageName,
                        },
                    },
                },
            },
        },
    }

    // Add ports if specified
    if len(ports) > 0 {
        var containerPorts []corev1.ContainerPort
        for _, port := range ports {
            protocol := corev1.ProtocolTCP
            if port.GetProtocol() == "UDP" {
                protocol = corev1.ProtocolUDP
            }
            containerPorts = append(containerPorts, corev1.ContainerPort{
                Name:          port.GetName(),
                ContainerPort: port.GetContainerPort(),
                Protocol:      protocol,
            })
        }
        deployment.Spec.Template.Spec.Containers[0].Ports = containerPorts
    }

    // Create Service if specified
    var service *corev1.Service
    if serviceConfig != nil {
        service = &corev1.Service{
            ObjectMeta: metav1.ObjectMeta{
                Name:        serviceName,
                Namespace:   namespace,
                Labels:      labels,
                Annotations: annotations,
            },
            Spec: corev1.ServiceSpec{
                Selector: labels,
            },
        }

        // Set service type
        switch serviceConfig.GetServiceType() {
        case "LoadBalancer":
            service.Spec.Type = corev1.ServiceTypeLoadBalancer
        case "NodePort":
            service.Spec.Type = corev1.ServiceTypeNodePort
        default:
            service.Spec.Type = corev1.ServiceTypeClusterIP
        }

        // Add ports if specified
        if len(serviceConfig.GetPorts()) > 0 {
            var servicePorts []corev1.ServicePort
            for _, port := range serviceConfig.GetPorts() {
                protocol := corev1.ProtocolTCP
                if port.GetProtocol() == "UDP" {
                    protocol = corev1.ProtocolUDP
                }
                // Use intstr.FromInt for TargetPort
                servicePorts = append(servicePorts, corev1.ServicePort{
                    Name:       port.GetName(),
                    Port:       port.GetPort(),
                    TargetPort: intstr.FromInt(int(port.GetTargetPort())),
                    Protocol:   protocol,
                })
            }
            service.Spec.Ports = servicePorts
        }
    }

    // Try to get the deployment first
    _, err := clientset.AppsV1().Deployments(namespace).Get(context.TODO(), deploymentName, metav1.GetOptions{})
    if err != nil {
        if errors.IsNotFound(err) {
            // Create the deployment if it doesn't exist
            deployment, err := clientset.AppsV1().Deployments(namespace).Create(context.TODO(), deployment, metav1.CreateOptions{})
            if err != nil {
                return "", "", fmt.Errorf("failed to create deployment: %v", err)
            }
            log.Printf("Deployment %s created", deployment.Name)
        } else {
            return "", "", fmt.Errorf("failed to get deployment: %v", err)
        }
    } else {
        // Update the deployment if it exists
        deployment, err := clientset.AppsV1().Deployments(namespace).Update(context.TODO(), deployment, metav1.UpdateOptions{})
        if err != nil {
            return "", "", fmt.Errorf("failed to update deployment: %v", err)
        }
        log.Printf("Deployment %s updated", deployment.Name)
    }

    // Create or update service if specified
    if service != nil {
        _, err := clientset.CoreV1().Services(namespace).Get(context.TODO(), serviceName, metav1.GetOptions{})
        if err != nil {
            if errors.IsNotFound(err) {
                _, err := clientset.CoreV1().Services(namespace).Create(context.TODO(), service, metav1.CreateOptions{})
                if err != nil {
                    return deploymentName, "", fmt.Errorf("failed to create service: %v", err)
                }
                log.Printf("Service %s created", serviceName)
            } else {
                return deploymentName, "", fmt.Errorf("failed to get service: %v", err)
            }
        } else {
            _, err := clientset.CoreV1().Services(namespace).Update(context.TODO(), service, metav1.UpdateOptions{})
            if err != nil {
                return deploymentName, "", fmt.Errorf("failed to update service: %v", err)
            }
            log.Printf("Service %s updated", serviceName)
        }
    }

    return deploymentName, serviceName, nil
}