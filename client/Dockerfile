# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install git for go modules
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the client binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o client ./client

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates netcat-openbsd

WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /app/client .

# Run the binary with default parameters
CMD ["./client", "--deploy", "--image", "todolistapp", "--app", "myapp", "--replicas", "2"]