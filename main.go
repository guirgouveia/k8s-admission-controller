package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var buildTime string
var gitCommit string

var (
	scheme  = runtime.NewScheme()
	codecs  = serializer.NewCodecFactory(scheme)
	port    = ":8443"
	certDir = "/certs/"
)

func init() {
	// Configure logging
	log.SetFormatter(&log.JSONFormatter{
		TimestampFormat: time.RFC3339Nano,
	})
	log.SetOutput(os.Stdout)

	// Set log level from environment variable
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel != "" {
		level, err := log.ParseLevel(logLevel)
		if err != nil {
			log.Warnf("Invalid log level %q, defaulting to info", logLevel)
			log.SetLevel(log.InfoLevel)
		} else {
			log.SetLevel(level)
		}
	} else {
		log.SetLevel(log.InfoLevel)
	}
}

func main() {
	log.WithFields(log.Fields{
		"port":     port,
		"certDir":  certDir,
		"certPath": certDir + "tls.crt",
		"keyPath":  certDir + "tls.key",
		"logLevel": log.GetLevel().String(),
	}).Info(fmt.Sprintf("Starting Admission Controller: version %s, build time %s", gitCommit, buildTime))

	// Create HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/mutate-pod-creation", handlePodCreation)
	mux.HandleFunc("/validate-pod-status", handlePodStatusChangeValidation)
	mux.HandleFunc("/healthz", handleHealth)
	mux.HandleFunc("/readyz", handleHealth)
	mux.HandleFunc("/livez", handleHealth)

	server := &http.Server{
		Addr:              port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       10 * time.Second,
	}

	if err := server.ListenAndServeTLS(certDir+"tls.crt", certDir+"tls.key"); err != nil {
		log.WithError(err).Fatal("Failed to start server")
	}
}

func handlePodStatusChangeValidation(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	logger := log.WithFields(log.Fields{
		"method":    r.Method,
		"path":      r.URL.Path,
		"remoteIP":  r.RemoteAddr,
		"userAgent": r.UserAgent(),
	})

	defer func() {
		if r := recover(); r != nil {
			logger.WithField("panic", r).Error("Recovered from panic in mutation handler")
			writeError(w, "Internal server error", http.StatusInternalServerError)
		}
		logger.WithField("duration", time.Since(startTime).String()).Info("Successfully validated status update request.")
	}()

	if r.Method != http.MethodPost {
		writeError(w, "Only POST requests are allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.Header.Get("Content-Type") != "application/json" {
		writeError(w, "Invalid content type, expecting application/json", http.StatusUnsupportedMediaType)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, fmt.Sprintf("Failed to read request body: %v", err), http.StatusInternalServerError)
		return
	}

	review, pod, err := parseAdmissionReview(body)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Start a goroutine to update pod labels without blocking the request
	go func() {
		if err := updatePodLabels(pod); err != nil {
			log.WithError(err).Error("Failed to update pod labels")
		}
	}()

	// Create admission response
	response := admissionv1.AdmissionResponse{
		UID:     review.Request.UID,
		Allowed: true,
	}

	// Send response
	reviewResponse := admissionv1.AdmissionReview{
		TypeMeta: review.TypeMeta,
		Response: &response,
	}

	respBytes, err := json.Marshal(reviewResponse)
	if err != nil {
		writeError(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(respBytes); err != nil {
		logger.WithError(err).Error("Failed to write response")
	}
}

// updatePodLabels patches a pod with the desired labels
func updatePodLabels(pod *corev1.Pod) error {
	// Create in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		writeError(nil, fmt.Sprintf("Failed to create in-cluster config: %v", err), http.StatusInternalServerError)
	}

	// Create Kubernetes client
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		writeError(nil, fmt.Sprintf("Failed to create Kubernetes client: %v", err), http.StatusInternalServerError)
		panic(err.Error())
	}

	// Watch for pod status changes until the pod has an IP address and node name
	// then patch the pod with the desired labels
	for {
		pod, err = clientset.CoreV1().Pods("default").Get(context.TODO(), pod.Name, metav1.GetOptions{})
		if err != nil {
			writeError(nil, fmt.Sprintf("Failed to get pod: %v", err), http.StatusInternalServerError)
		}

		if pod.Status.PodIP != "" && pod.Spec.NodeName != "" {
			labels := map[string]string{
				"missingLabelsValues": "false",
				"ipAddress":           pod.Status.PodIP,
				"nodeName":            pod.Spec.NodeName,
			}

			patchData, err := json.Marshal(map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": labels,
				},
			})
			if err != nil {
				writeError(nil, fmt.Sprintf("Failed to marshal patch data: %v", err), http.StatusInternalServerError)
			}

			_, err = clientset.CoreV1().Pods(pod.Namespace).Patch(
				context.TODO(),
				pod.Name,
				types.MergePatchType,
				patchData,
				metav1.PatchOptions{},
			)
			if err != nil {
				writeError(nil, fmt.Sprintf("Failed to patch pod: %v", err), http.StatusInternalServerError)
			}

			break
		}
	}

	// Define labels to add
	labels := map[string]string{
		"environment": "production",
		"ipAddress":   pod.Status.PodIP,
		"nodeName":    pod.Spec.NodeName,
	}

	// Create JSON patch for labels
	patchData, err := json.Marshal(map[string]interface{}{
		"metadata": map[string]interface{}{
			"labels": labels,
		},
	})
	if err != nil {
		writeError(nil, fmt.Sprintf("Failed to marshal patch data: %v", err), http.StatusInternalServerError)
	}

	// Patch the pod using the correct type from `types` package
	_, err = clientset.CoreV1().Pods(pod.Namespace).Patch(
		context.TODO(),
		pod.Name,
		types.MergePatchType,
		patchData,
		metav1.PatchOptions{},
	)
	if err != nil {
		writeError(nil, fmt.Sprintf("Failed to patch pod: %v", err), http.StatusInternalServerError)
	}

	log.Printf("Successfully patched pod %s/%s with labels: %v", pod.Namespace, pod.Name, labels)

	return nil
}
func handleHealth(w http.ResponseWriter, r *http.Request) {
	urlPath := r.URL.Path
	logger := log.WithFields(log.Fields{
		"method": r.Method,
		"path":   urlPath,
	})

	// Determine if this is a liveness or readiness probe
	probeType := "health"
	if urlPath == "/readyz" {
		probeType = "readiness"
	} else if urlPath == "/livez" {
		probeType = "liveness"
	}

	logger.WithField("probeType", probeType).Info("Health check request received")

	// For readiness check, verify we can process requests
	if probeType == "readiness" || probeType == "health" {
		// Check if server is accepting connections
		_, err := net.DialTimeout("tcp", port, 1*time.Second)
		if err != nil {
			logger.WithError(err).Error("Readiness probe failed: cannot accept connections")
			http.Error(w, "Not ready", http.StatusServiceUnavailable)
			return
		}
	}

	// For liveness check, verify critical components
	if probeType == "liveness" || probeType == "health" {
		// Verify TLS files exist
		if _, err := os.Stat(certDir + "tls.crt"); err != nil {
			msg := "TLS certificate not found"
			logger.WithError(err).Error(msg)
			http.Error(w, msg, http.StatusInternalServerError)
			return
		}

		if _, err := os.Stat(certDir + "tls.key"); err != nil {
			msg := "TLS key not found"
			logger.WithError(err).Error(msg)
			http.Error(w, msg, http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
	logger.Info("Health check completed successfully")
}

func parseAdmissionReview(body []byte) (*admissionv1.AdmissionReview, *corev1.Pod, error) {
	if len(body) == 0 {
		return nil, nil, fmt.Errorf("empty request body")
	}

	review := admissionv1.AdmissionReview{}
	_, _, err := codecs.UniversalDeserializer().Decode(body, nil, &review)
	if err != nil {
		return nil, nil, fmt.Errorf("could not decode request body: %v", err)
	}

	if review.Request == nil {
		return nil, nil, fmt.Errorf("admission review request is nil")
	}

	if review.Request.Kind.Kind != "Pod" {
		return nil, nil, fmt.Errorf("only supports Pod mutations, got %s", review.Request.Kind.Kind)
	}

	pod := corev1.Pod{}
	if err := json.Unmarshal(review.Request.Object.Raw, &pod); err != nil {
		return nil, nil, fmt.Errorf("failed to decode pod object: %v", err)
	}

	return &review, &pod, nil
}

func writeError(w http.ResponseWriter, message string, code int) {
	log.WithFields(log.Fields{
		"code":    code,
		"message": message,
		"time":    time.Now().Format(time.RFC3339Nano),
	}).Error("Admission controller error")
	http.Error(w, message, code)
}

// createPatch generates a JSON patch for updating pod labels
func createPatch(pod *corev1.Pod, labels map[string]string, logger *log.Entry) string {
	var operations []string

	if pod.Labels == nil {
		operations = append(operations, `{"op":"add","path":"/metadata/labels","value":{}}`)
		pod.Labels = make(map[string]string)
	}

	// Helper function to add or replace label
	addOrReplaceLabel := func(name, value string) {

		if pod.Labels != nil {
			// Add or remove missingLabelsValues label based on pending status of both ipAddress and nodeName
			if value == "pending" {
				operations = append(operations, `{"op":"add","path":"/metadata/labels/missingLabelsValues","value":"true"}`)
			} else if pod.Labels["missingLabelsValues"] == "true" && (labels["ipAddress"] != "pending" && labels["nodeName"] != "pending") {
				// remove missingLabelsValues label if both ipAddress and nodeName are not pending
				operations = append(operations, `{"op":"remove","path":"/metadata/labels/missingLabelsValues"}`)
			}

			if _, exists := pod.Labels[name]; exists {
				// Label exists, replace it
				operations = append(operations, fmt.Sprintf(`{"op":"replace","path":"/metadata/labels/%s","value":"%s"}`, name, value))
			} else {
				// Label doesn't exist, add it
				operations = append(operations, fmt.Sprintf(`{"op":"add","path":"/metadata/labels/%s","value":"%s"}`, name, value))
			}
		} else {
			// No labels map, just add the label
			operations = append(operations, fmt.Sprintf(`{"op":"add","path":"/metadata/labels/%s","value":"%s"}`, name, value))
		}
	}

	// Iterate over labels and add/replace each one
	for name, value := range labels {
		addOrReplaceLabel(name, value)
	}

	// Create the final patch
	patch := fmt.Sprintf("[%s]", strings.Join(operations, ","))

	logger.WithField("patch", patch).Debug("Generated JSON patch")
	return patch
}

func handlePodCreation(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	logger := log.WithFields(log.Fields{
		"method":    r.Method,
		"path":      r.URL.Path,
		"remoteIP":  r.RemoteAddr,
		"userAgent": r.UserAgent(),
	})

	defer func() {
		if r := recover(); r != nil {
			logger.WithField("panic", r).Error("Recovered from panic in mutation handler")
			writeError(w, "Internal server error", http.StatusInternalServerError)
		}
		logger.WithField("duration", time.Since(startTime).String()).Info("Successfully processed mutating request.")
	}()

	if r.Method != http.MethodPost {
		writeError(w, "Only POST requests are allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.Header.Get("Content-Type") != "application/json" {
		writeError(w, "Invalid content type, expecting application/json", http.StatusUnsupportedMediaType)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, fmt.Sprintf("Failed to read request body: %v", err), http.StatusInternalServerError)
		return
	}

	review, pod, err := parseAdmissionReview(body)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	logger = logger.WithFields(log.Fields{
		"uid":       review.Request.UID,
		"namespace": pod.Namespace,
		"name":      pod.Name,
	})
	logger.Info("Processing pod creation request.")

	// Determine owning resource type
	owningResource := "None"
	if len(pod.OwnerReferences) > 0 {
		owner := pod.OwnerReferences[0].Kind
		if owner == "ReplicaSet" {
			owningResource = "ReplicaSet"
		} else if owner == "StatefulSet" {
			owningResource = "StatefulSet"
		} else if owner == "Job" {
			owningResource = "Job"
		}
	}

	// Get IP address and node name
	ipAddress := pod.Status.PodIP
	nodeName := pod.Spec.NodeName

	if ipAddress == "" {
		ipAddress = "pending"
	}

	if nodeName == "" {
		nodeName = "pending"
	}

	logger = logger.WithFields(log.Fields{
		"environment":    "production",
		"owningResource": owningResource,
		"ipAddress":      ipAddress,
		"nodeName":       nodeName,
	})

	// Define the labels to be added/replaced
	labels := map[string]string{
		"environment":    "production",
		"owningResource": owningResource,
		"ipAddress":      ipAddress,
		"nodeName":       nodeName,
	}

	// Generate the patch
	patch := createPatch(pod, labels, logger)

	// Create admission response
	response := admissionv1.AdmissionResponse{
		UID:     review.Request.UID,
		Allowed: true,
		Patch:   []byte(patch),
		PatchType: func() *admissionv1.PatchType {
			t := admissionv1.PatchTypeJSONPatch
			return &t
		}(),
	}

	// Send response
	reviewResponse := admissionv1.AdmissionReview{
		TypeMeta: review.TypeMeta,
		Response: &response,
	}

	respBytes, err := json.Marshal(reviewResponse)
	if err != nil {
		writeError(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(respBytes); err != nil {
		writeError(w, fmt.Sprintf("Failed to write response: %v", err), http.StatusInternalServerError)
		return
	}
}
