package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"time"

	pxapi "github.com/Telmate/proxmox-api-go/proxmox"
	"github.com/go-redis/redis/v8"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type HelmRelease struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Chart     string `json:"chart"`
	Version   string `json:"version"`
	Status    string `json:"status"`
}

type NodeStatus struct {
	Name       string   `json:"name"`
	Status     string   `json:"status"`
	Roles      []string `json:"roles"`
	Version    string   `json:"version"`
	InternalIP string   `json:"internalIP"`
	Uptime     string   `json:"uptime"`
}

type ProxmoxNode struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Uptime string `json:"uptime"`
}

type PodStatuses struct {
	Running   int `json:"Running"`
	Pending   int `json:"Pending"`
	Failed    int `json:"Failed"`
	Succeeded int `json:"Succeeded"`
	Unknown   int `json:"Unknown"`
}

var (
	redisClient *redis.Client
	ctx         = context.Background()
	useRedis    bool
	cacheTTL    time.Duration
)

func main() {
	initRedis()

	http.HandleFunc("/status", statusHandler)
	http.HandleFunc("/healthz", healthzHandler)
	http.HandleFunc("/ready", readyHandler)
	
	log.Println("Server starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func initRedis() {
	redisEnabled, _ := strconv.ParseBool(os.Getenv("REDIS_ENABLED"))
	if !redisEnabled {
		useRedis = false
		log.Println("Redis is disabled")
		return
	}

	redisHost := os.Getenv("REDIS_HOST")
	redisPort := os.Getenv("REDIS_PORT")
	if redisHost == "" || redisPort == "" {
		log.Println("REDIS_HOST or REDIS_PORT environment variable is not set, disabling Redis")
		useRedis = false
		return
	}

	ttl, err := strconv.Atoi(os.Getenv("REDIS_TTL"))
	if err != nil {
		log.Println("Invalid REDIS_TTL, using default of 5 seconds")
		ttl = 5
	}
	cacheTTL = time.Duration(ttl) * time.Second

	redisClient = redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%s", redisHost, redisPort),
	})

	_, err = redisClient.Ping(ctx).Result()
	if err != nil {
		log.Printf("Failed to connect to Redis: %v, disabling Redis", err)
		useRedis = false
		return
	}
	
	useRedis = true
	log.Println("Connected to Redis successfully")
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	helmReleasesRaw, err := getDataWithCache("helm_releases", func() (interface{}, error) {
		return getHelmReleases()
	})
	if err != nil {
		http.Error(w, "Failed to get Helm releases: "+err.Error(), http.StatusInternalServerError)
		return
	}

	nodeStatusesRaw, err := getDataWithCache("node_statuses", func() (interface{}, error) {
		return getNodeStatuses()
	})
	if err != nil {
		http.Error(w, "Failed to get node statuses: "+err.Error(), http.StatusInternalServerError)
		return
	}

	proxmoxNodesRaw, err := getDataWithCache("proxmox_nodes", func() (interface{}, error) {
		return getProxmoxNodes()
	})
	if err != nil {
		http.Error(w, "Failed to get Proxmox nodes: "+err.Error(), http.StatusInternalServerError)
		return
	}

	podStatusesRaw, err := getDataWithCache("pod_statuses", func() (interface{}, error) {
		return getPodStatuses()
	})
	if err != nil {
		http.Error(w, "Failed to get pod statuses: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Правильное преобразование типов
	helmReleases, ok := helmReleasesRaw.([]HelmRelease)
	if !ok {
		log.Printf("Failed to convert helmReleasesRaw to []HelmRelease: %v", helmReleasesRaw)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	nodeStatuses, ok := nodeStatusesRaw.([]NodeStatus)
	if !ok {
		log.Printf("Failed to convert nodeStatusesRaw to []NodeStatus: %v", nodeStatusesRaw)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	proxmoxNodes, ok := proxmoxNodesRaw.([]ProxmoxNode)
	if !ok {
		log.Printf("Failed to convert proxmoxNodesRaw to []ProxmoxNode: %v", proxmoxNodesRaw)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	podStatuses, ok := podStatusesRaw.(PodStatuses)
	if !ok {
		log.Printf("Failed to convert podStatusesRaw to PodStatuses: %v", podStatusesRaw)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	response := struct {
		HelmReleases []HelmRelease `json:"helmReleases"`
		NodeStatuses []NodeStatus  `json:"nodeStatuses"`
		ProxmoxNodes []ProxmoxNode `json:"proxmoxNodes"`
		PodStatuses  PodStatuses   `json:"podStatuses"`
	}{
		HelmReleases: helmReleases,
		NodeStatuses: nodeStatuses,
		ProxmoxNodes: proxmoxNodes,
		PodStatuses:  podStatuses,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func getDataWithCache(cacheKey string, getData func() (interface{}, error)) (interface{}, error) {
	if !useRedis {
		return getData()
	}

	cachedData, err := redisClient.Get(ctx, cacheKey).Result()
	if err == nil {
		var data interface{}
		err = json.Unmarshal([]byte(cachedData), &data)
		if err == nil {
			return data, nil
		}
	}

	data, err := getData()
	if err != nil {
		return nil, err
	}

	cacheData, _ := json.Marshal(data)
	redisClient.Set(ctx, cacheKey, cacheData, cacheTTL)

	return data, nil
}


func getCachedHelmReleases() ([]HelmRelease, error) {
	cacheKey := "helm_releases"
	cachedData, err := redisClient.Get(ctx, cacheKey).Result()
	if err == nil {
		var releases []HelmRelease
		err = json.Unmarshal([]byte(cachedData), &releases)
		if err == nil {
			return releases, nil
		}
	}

	releases, err := getHelmReleases()
	if err != nil {
		return nil, err
	}

	cacheData, _ := json.Marshal(releases)
	redisClient.Set(ctx, cacheKey, cacheData, 5*time.Second)

	return releases, nil
}

func getCachedNodeStatuses() ([]NodeStatus, error) {
	cacheKey := "node_statuses"
	cachedData, err := redisClient.Get(ctx, cacheKey).Result()
	if err == nil {
		var statuses []NodeStatus
		err = json.Unmarshal([]byte(cachedData), &statuses)
		if err == nil {
			return statuses, nil
		}
	}

	statuses, err := getNodeStatuses()
	if err != nil {
		return nil, err
	}

	cacheData, _ := json.Marshal(statuses)
	redisClient.Set(ctx, cacheKey, cacheData, 5*time.Second)

	return statuses, nil
}

func getCachedProxmoxNodes() ([]ProxmoxNode, error) {
	cacheKey := "proxmox_nodes"
	cachedData, err := redisClient.Get(ctx, cacheKey).Result()
	if err == nil {
		var nodes []ProxmoxNode
		err = json.Unmarshal([]byte(cachedData), &nodes)
		if err == nil {
			return nodes, nil
		}
	}

	nodes, err := getProxmoxNodes()
	if err != nil {
		return nil, err
	}

	cacheData, _ := json.Marshal(nodes)
	redisClient.Set(ctx, cacheKey, cacheData, 5*time.Second)

	return nodes, nil
}

func getCachedPodStatuses() (PodStatuses, error) {
	cacheKey := "pod_statuses"
	cachedData, err := redisClient.Get(ctx, cacheKey).Result()
	if err == nil {
		var statuses PodStatuses
		err = json.Unmarshal([]byte(cachedData), &statuses)
		if err == nil {
			return statuses, nil
		}
	}

	statuses, err := getPodStatuses()
	if err != nil {
		return PodStatuses{}, err
	}

	cacheData, _ := json.Marshal(statuses)
	redisClient.Set(ctx, cacheKey, cacheData, 5*time.Second)

	return statuses, nil
}

func getHelmReleases() ([]HelmRelease, error) {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		return nil, fmt.Errorf("KUBECONFIG environment variable is not set")
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to build config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	namespaces, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	var allReleases []HelmRelease

	for _, ns := range namespaces.Items {
		settings := cli.New()
		actionConfig := new(action.Configuration)
		if err := actionConfig.Init(settings.RESTClientGetter(), ns.Name, os.Getenv("HELM_DRIVER"), log.Printf); err != nil {
			log.Printf("Failed to initialize action configuration for namespace %s: %v", ns.Name, err)
			continue
		}

		listAction := action.NewList(actionConfig)
		releases, err := listAction.Run()
		if err != nil {
			log.Printf("Failed to list releases in namespace %s: %v", ns.Name, err)
			continue
		}

		for _, r := range releases {
			allReleases = append(allReleases, HelmRelease{
				Name:      r.Name,
				Namespace: r.Namespace,
				Chart:     r.Chart.Metadata.Name,
				Version:   r.Chart.Metadata.Version,
				Status:    r.Info.Status.String(),
			})
		}
	}

	return allReleases, nil
}

func getNodeStatuses() ([]NodeStatus, error) {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		return nil, fmt.Errorf("KUBECONFIG environment variable is not set")
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to build config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	var nodeStatuses []NodeStatus
	for _, node := range nodes.Items {
		var internalIP string
		for _, addr := range node.Status.Addresses {
			if addr.Type == "InternalIP" {
				internalIP = addr.Address
				break
			}
		}

		uptime := formatUptime(time.Since(node.CreationTimestamp.Time))

		status := "Unknown"
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady {
				if condition.Status == corev1.ConditionTrue {
					status = "Ready"
				} else {
					status = "NotReady"
				}
				break
			}
		}

		nodeStatuses = append(nodeStatuses, NodeStatus{
			Name:       node.Name,
			Status:     status,
			Roles:      getRoles(node.Labels),
			Version:    node.Status.NodeInfo.KubeletVersion,
			InternalIP: internalIP,
			Uptime:     uptime,
		})
	}

	return nodeStatuses, nil
}

func getRoles(labels map[string]string) []string {
	var roles []string
	if _, isControlPlane := labels["node-role.kubernetes.io/control-plane"]; isControlPlane {
		roles = append(roles, "control-plane")
	}
	if _, isMaster := labels["node-role.kubernetes.io/master"]; isMaster {
		roles = append(roles, "master")
	}
	if _, isWorker := labels["node-role.kubernetes.io/worker"]; isWorker {
		roles = append(roles, "worker")
	}
	if len(roles) == 0 {
		roles = append(roles, "worker") // Assume worker if no specific role is set
	}
	return roles
}

func getProxmoxNodes() ([]ProxmoxNode, error) {
	proxmoxAPI := os.Getenv("PROXMOX_API_URL")
	proxmoxUser := os.Getenv("PROXMOX_USER")
	proxmoxPassword := os.Getenv("PROXMOX_PASSWORD")

	if proxmoxAPI == "" || proxmoxUser == "" || proxmoxPassword == "" {
		return nil, fmt.Errorf("Proxmox environment variables are not set")
	}

	tlsConfig := &tls.Config{InsecureSkipVerify: true}
	client, err := pxapi.NewClient(proxmoxAPI, nil, "", tlsConfig, "", 300)
	if err != nil {
		return nil, fmt.Errorf("failed to create Proxmox client: %w", err)
	}

	err = client.Login(proxmoxUser, proxmoxPassword, "")
	if err != nil {
		return nil, fmt.Errorf("failed to login to Proxmox: %w", err)
	}

	nodeList, err := client.GetNodeList()
	if err != nil {
		return nil, fmt.Errorf("failed to get Proxmox nodes: %w", err)
	}

	data, ok := nodeList["data"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected node list data type: %T", nodeList["data"])
	}

	var proxmoxNodes []ProxmoxNode
	for _, n := range data {
		node, ok := n.(map[string]interface{})
		if !ok {
			log.Printf("Unexpected node type: %T", n)
			continue
		}

		status := "unknown"
		if s, ok := node["status"].(string); ok {
			status = s
		}

		uptime := "unknown"
		if u, ok := node["uptime"].(float64); ok {
			uptime = formatUptime(time.Duration(u) * time.Second)
		}

		name, _ := node["node"].(string)

		proxmoxNodes = append(proxmoxNodes, ProxmoxNode{
			Name:   name,
			Status: status,
			Uptime: uptime,
		})
	}

	return proxmoxNodes, nil
}

func getPodStatuses() (PodStatuses, error) {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		return PodStatuses{}, fmt.Errorf("KUBECONFIG environment variable is not set")
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return PodStatuses{}, fmt.Errorf("failed to build config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return PodStatuses{}, fmt.Errorf("failed to create clientset: %w", err)
	}

	pods, err := clientset.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return PodStatuses{}, fmt.Errorf("failed to list pods: %w", err)
}

statuses := PodStatuses{}
for _, pod := range pods.Items {
	switch pod.Status.Phase {
	case corev1.PodRunning:
		statuses.Running++
	case corev1.PodPending:
		statuses.Pending++
	case corev1.PodFailed:
		statuses.Failed++
	case corev1.PodSucceeded:
		statuses.Succeeded++
	default:
		statuses.Unknown++
	}
}

return statuses, nil
}

func formatUptime(duration time.Duration) string {
days := duration.Hours() / 24
return fmt.Sprintf("%.2f days", math.Floor(days*100)/100)
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func readyHandler(w http.ResponseWriter, r *http.Request) {
	if useRedis {
		// Проверка подключения к Redis
		_, err := redisClient.Ping(ctx).Result()
		if err != nil {
			http.Error(w, "Redis connection failed", http.StatusServiceUnavailable)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Ready"))
}