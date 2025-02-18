# 🚀 Kubernetes Admission Controller - Label Mutating Webhook

A Kubernetes **Mutating Admission Webhook** that automatically adds standardized labels to newly created pods, improving cluster organization and resource tracking.

## 📌 What It Does

This admission controller automatically assigns the following labels to every new pod in your cluster:

| Label | Description | Example |
|-------|-------------|---------|
| `environment` | Identifies the pod's environment | `production` |
| `owningResource` | Indicates the resource managing the pod | `ReplicaSet`, `StatefulSet`, `Job`, `None` |
| `ipAddress` | Stores the pod's IP address | Initially `pending`, then actual IP |
| `nodeName` | Specifies the node hosting the pod | Initially `pending`, then node name |

## 🔍 How It Works

1. **Pod Creation Request**: When a pod creation request is made to the Kubernetes API server
2. **Webhook Invocation**: The API server forwards the request to our webhook
3. **Label Processing**:
   - Checks for existing labels
   - Determines owning resource
   - Gets pod IP and node name
   - Creates JSON patch for missing labels
4. **Pod Creation**: API server applies the patch and creates the pod

## 📁 Project Structure

```
.
├── Dockerfile                # Container image definition
├── main.go                   # Webhook implementation
├── go.mod                    # Go module file
├── README.md                # Documentation
└── manifests/               # Kubernetes manifests
    ├── cert-manager.yaml     # Certificate configuration
    ├── controller.yaml       # Webhook deployment
    ├── mutating-webhook.yaml # Webhook configuration
    └── pod.yaml             # Test pod manifest
```

## 🔧 Prerequisites

- Kubernetes cluster (v1.16+)
- kubectl configured
- cert-manager installed
- Docker for building images

## 📦 Installation

### 1. Clone the Repository
```bash
git clone https://github.com/yourusername/k8s-admission-controller.git
cd k8s-admission-controller
```

### 2. Build and Push the Docker Image
```bash
# Build the image
docker build -t jumads/admission-controller:latest .

# Push to registry
docker push jumads/admission-controller:latest
```

### 3. Deploy to Kubernetes

```bash
# Install cert-manager (if not already installed)
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.12.0/cert-manager.yaml

# Wait for cert-manager to be ready
kubectl wait --for=condition=ready pod -l app=cert-manager -n cert-manager

# Deploy the webhook
kubectl apply -f manifests/cert-manager.yaml
kubectl apply -f manifests/controller.yaml
kubectl apply -f manifests/mutating-webhook.yaml
```

## 🧪 Testing

### Create a Test Pod
```bash
# Apply the test pod
kubectl apply -f manifests/pod.yaml

# Check the labels
kubectl get pod test-pod --show-labels
```

Expected output:
```
NAME       READY   STATUS    RESTARTS   AGE   LABELS
test-pod   1/1     Running   0          1m    environment=production,owningResource=None,ipAddress=10.244.0.15,nodeName=worker-1
```

## 🔒 Security Features

- **TLS Encryption**: Secure webhook communication
- **Certificate Management**: Automated by cert-manager
- **Namespace Filtering**: Excludes system namespaces
- **Failure Policy**: Fails closed for security
- **Resource Limits**: Prevents resource exhaustion

## 🚀 Development

### Adding New Labels
```go
// In main.go
labels := map[string]string{
    "environment":    "production",
    "owningResource": owningResource,
    "ipAddress":      ipAddress,
    "nodeName":       nodeName,
    // Add new labels here
}
```

### Local Testing
```bash
# Build
go build -o admission-controller

# Test
go test ./...
```