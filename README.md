# 🚀 Kubernetes Resource Mutation Strategies

This repository explores different approaches to mutating Kubernetes resources, including:

1. **Mutating Admission Webhook**: Automatically adds standardized labels to newly created pods
2. **Operator Pattern**: Uses Operator SDK to build a controller that manages pod labels
3. **MutatingWebhookPolicy**: Implements resource mutation through admission policies

> **Note**: The Operator and MutatingWebhookPolicy implementations are currently under the `development` branch.

## 📌 What It Does

These implementations automatically assign the following labels to every new pod in your cluster:

| Label          | Description                          | Example         |
|----------------|--------------------------------------|-----------------|
| `environment`  | Identifies the pod's environment     | `production`    |
| `owningResource`| Indicates the resource managing the pod | `ReplicaSet`, `StatefulSet`, `Job`, `None` |
| `ipAddress`    | Stores the pod's IP address          | Initially `pending`, then actual IP |
| `nodeName`     | Specifies the node hosting the pod   | Initially `pending`, then node name |

## 🔍 How It Works

### Mutating Admission Webhook

1. **Pod Creation Request**: When a pod creation request is made to the Kubernetes API server
2. **Webhook Invocation**: The API server forwards the request to our webhook
3. **Label Processing**:
   - Checks for existing labels
   - Determines owning resource
   - Gets pod IP and node name
   - Creates JSON patch for missing labels
4. **Pod Creation**: API server applies the patch and creates the pod

### Operator Pattern

1. **Pod Creation**: Pod is created without labels
2. **Controller Watch**: Operator detects new pod
3. **Label Reconciliation**:
   - Adds missing labels
   - Updates labels as pod status changes
4. **Continuous Monitoring**: Operator maintains labels throughout pod lifecycle

## 📁 Project Structure

The repository is organized as follows:

```
k8s-admission-controller/
├── Dockerfile              # Container image definition
├── README.md              # Project documentation
├── go.mod                 # Go module definition
├── go.sum                 # Go dependencies checksum
├── main.go                # Main application code
├── manifests/             # Kubernetes resource definitions
│   ├── audit-policy.yaml      # Kubernetes audit policy
│   ├── cert-manager.yaml      # Certificate management
│   ├── controller.yaml        # Controller configuration
│   ├── deployment.yaml        # Application deployment
│   ├── mutating-webhook.yaml  # Webhook configuration
│   ├── network-policy.yaml    # Network policies
│   ├── pod.yaml              # Sample pod configuration
│   ├── rbac.yaml             # RBAC permissions
│   └── validating-webhook.yaml # Validation webhook
├── operators/             # Kubernetes operators
│   └── pod-labels-operator   # Pod labeling operator
├── policies/             # Admission control policies
│   ├── admission-policy.yaml # General admission rules
│   └── mutation-policy.yaml  # Mutation rules
└── skaffold.yaml         # Skaffold CI/CD configuration
```

## 🚀 Getting Started

To deploy the Mutating Admission Webhook in your Kubernetes cluster, follow these steps:

### Prerequisites

- Kubernetes cluster (v1.16 or later. )
- `kubectl` configured to interact with your cluster
- Docker (for building the controller image)

>
> _To utilize the new MutatingAdmissionPolicy, which simplifies the admission processes a lot, instead of the MutatingAdmissionConfiguration ensure your Kubernetes cluster is running version 1.32 or newer._ 
>

### Steps

1. **Clone the Repository**

    ```sh
    git clone https://github.com/guirgouveia/k8s-admission-controller.git
    cd k8s-admission-controller
    ```

2. **Build and Push the Docker Image**

    Build the Docker image for the admission controller:

    ```sh
    docker build -t your-registry/k8s-admission-controller:latest .
    ```

    Push the image to your container registry:

    ```sh
    docker push your-registry/k8s-admission-controller:latest
    ```

    Replace `your-registry` with the appropriate registry URL.

3. **Deploy the Admission Controller**

    Update the image field in `kubernetes/deployment.yaml` to reference your Docker image.

    Apply the Kubernetes manifests:

    ```sh
    kubectl apply -f kubernetes/service.yaml
    kubectl apply -f kubernetes/deployment.yaml
    kubectl apply -f kubernetes/webhook-config.yaml
    ```

4. **Verify the Deployment**

    Ensure the admission controller pod is running:

    ```sh
    kubectl get pods -l app=k8s-admission-controller
    ```

    Check for the `Running` status.

See [Skaffold Section](#local-development-with-skaffold) below for local development instructions.

## 🛠️ Development

### Local Development with Skaffold

For a smoother development experience, this project uses Skaffold for local CI/CD. Start the development environment with:

```sh
skaffold dev --keep-running-on-failure=true
```

This command:
- Watches for file changes
- Rebuilds the container image
- Updates the Kubernetes deployment
- Keeps pods running even if they crash (useful for debugging)
- Shows real-time logs from all pods

To temporarily disable auto-rebuilds while debugging:

```sh
# Press Ctrl+Z to pause
# Press Ctrl+Z again to resume
```

### Manual Deployment

To contribute or modify the admission controller:

1. Make your code changes in `main.go`.
2. Rebuild and push the Docker image.
3. Update the deployment in your cluster:

    ```sh
    kubectl rollout restart deployment k8s-admission-controller
    ```