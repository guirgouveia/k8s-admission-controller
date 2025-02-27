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

### MutatingWebhookPolicy Implementation

Starting from Kubernetes 1.32+, you can use the new MutatingAdmissionPolicy feature:

1. **Policy Definition**: Define mutation rules declaratively in YAML
2. **No Webhook Server**: No need to maintain a separate webhook service
3. **Built-in Validation**: Kubernetes validates policy syntax
4. **Performance**: Better performance as mutations happen in-process

Example policy:
```yaml
apiVersion: admissionregistration.k8s.io/v1beta1
kind: MutatingAdmissionPolicy
metadata:
  name: add-pod-labels
spec:
  failurePolicy: Ignore
  matchConstraints:
    resourceRules:
    - apiGroups: [""]
      apiVersions: ["v1"]
      operations: ["CREATE"]
      resources: ["pods"]
```

### Operator SDK Implementation

The Operator SDK version provides additional features:

1. **Reconciliation Loop**: Continuously ensures desired state
2. **Custom Resource Support**: Define PodLabelPolicy CRD
3. **Controller Runtime**: Built on controller-runtime library
4. **Metrics**: Built-in prometheus metrics
5. **Leader Election**: Automatic HA support



Example Custom Resource:
```yaml
apiVersion: labels.example.com/v1alpha1
kind: PodLabelPolicy
metadata:
  name: default-labels
spec:
  environment: production
  labels:
    team: platform
    cost-center: platform-engineering
```

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

- Kubernetes cluster (v1.16 or later)
- `kubectl` configured to interact with your cluster
- Docker (for building the controller image)
- Skaffold v2.0.0 or later (for local development)

### Development Workflow

#### Local Development with Skaffold

This project uses Skaffold to streamline the development workflow. It builds and deploys code to Docker Desktop, but you can specify your Kubernetes context in the [skaffold.yaml](./skaffold.yaml) file.

Start coding with:

```sh
 skaffold dev  --keep-running-on-failure=true --tail=false --interactive=false
```

or leverage the Taskfile for a more streamlined experience:

```sh
task run
```

Key features:
- Hot reload on code changes
- Real-time log streaming
- Automatic image builds
- Fast deployment updates
- Keep pods alive for debugging

Development commands:
```sh
# Start development with debug info
skaffold dev -v debug --keep-running-on-failure=true

# Run once without watching for changes
skaffold run

# Debug with port forwarding
skaffold debug --port-forward

# Clean up all deployed resources
skaffold delete
```

#### Manual Deployment

If you prefer not to use Skaffold, follow these steps:

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

See [Skaffold Section](#local-development-with-skaffold) above for local development instructions.

## 🛠️ Development

### Manual Deployment

To contribute or modify the admission controller:

1. Make your code changes in `main.go`.
2. Rebuild and push the Docker image.
3. Update the deployment in your cluster:

    ```sh
    kubectl rollout restart deployment k8s-admission-controller
    ```
