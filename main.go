package main

import (
	"encoding/json"
	"fmt"
	"io"
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
)

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
		"port":      port,
		"certDir":   certDir,
		"certPath":  certDir + "tls.crt",
		"keyPath":   certDir + "tls.key",
		"logLevel":  log.GetLevel().String(),
		"version":   "v1", // Add version info
		"buildTime": os.Getenv("BUILD_TIME"),
	}).Info("Starting Admission Controller")

	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", handleMutation)
	mux.HandleFunc("/validate", handleValidation)
	mux.HandleFunc("/healthz", handleHealth)

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

func handleHealth(w http.ResponseWriter, r *http.Request) {
	logger := log.WithFields(log.Fields{
		"method": r.Method,
		"path":   r.URL.Path,
	})
	logger.Info("Health check request received")

	// Verify TLS cert exists
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

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
	logger.Info("Health check request completed successfully")
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

	// Ensure labels map exists
	if pod.Labels == nil {
		operations = append(operations, `{"op":"add","path":"/metadata/labels","value":{}}`)
	}

	// Helper function to add or replace label
	addOrReplaceLabel := func(name, value string) {
		if pod.Labels != nil {
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

func handleMutation(w http.ResponseWriter, r *http.Request) {
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
		logger.WithField("duration", time.Since(startTime).String()).Info("Request completed")
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
	logger.Info("Processing admission request")

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

	// if ipAddress == "" || nodeName == "" {
	// 	response := admissionv1.AdmissionResponse{
	// 		UID:     review.Request.UID,
	// 		Allowed: false,
	// 		Result: &metav1.Status{
	// 			Message: "Pod IP or Node name not assigned yet. Retry later.",
	// 			Reason:  metav1.StatusReasonInvalid,
	// 		},
	// 	}

	// 	reviewResponse := admissionv1.AdmissionReview{
	// 		TypeMeta: review.TypeMeta,
	// 		Response: &response,
	// 	}

	// 	respBytes, err := json.Marshal(reviewResponse)
	// 	if err != nil {
	// 		writeError(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
	// 		return
	// 	}

	// 	logger.Info("Rejected admission request - missing IP or NodeName")
	// 	w.Header().Set("Content-Type", "application/json")
	// 	w.Write(respBytes)
	// 	return
	// }

	logger = logger.WithFields(log.Fields{
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
		logger.WithError(err).Error("Failed to write response")
	}
	logger.Info("Successfully processed admission request")
}

// validatePod checks if the pod has required fields
func validatePod(pod *corev1.Pod) error {
	if pod.Status.PodIP == "" {
		return fmt.Errorf("pod IP is not assigned")
	}

	if pod.Spec.NodeName == "" {
		return fmt.Errorf("node name is not assigned")
	}

	return nil
}

func handleValidation(w http.ResponseWriter, r *http.Request) {
	logger := log.WithFields(log.Fields{
		"method":    r.Method,
		"path":      r.URL.Path,
		"remoteIP":  r.RemoteAddr,
		"userAgent": r.UserAgent(),
	})

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

	// Validate pod
	if err := validatePod(pod); err != nil {
		response := admissionv1.AdmissionResponse{
			UID:     review.Request.UID,
			Allowed: false,
			Result: &metav1.Status{
				Message: fmt.Sprintf("Validation failed: %v", err),
				Reason:  metav1.StatusReasonInvalid,
			},
		}
		reviewResponse := admissionv1.AdmissionReview{
			TypeMeta: review.TypeMeta,
			Response: &response,
		}
		respBytes, err := json.Marshal(reviewResponse)
		if err != nil {
			writeError(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
			return
		}
		logger.Error("Pod has no IP or NodeName Set. Rejecting creation.")
		w.Header().Set("Content-Type", "application/json")
		w.Write(respBytes)
		return
	}

	// Pod is valid
	response := admissionv1.AdmissionResponse{
		UID:     review.Request.UID,
		Allowed: true,
	}
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
	w.Write(respBytes)
	logger.Info("Successfully validated pod")
}
