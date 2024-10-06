import (
    "context"
    "crypto/tls"
    "encoding/json"
    "fmt"
    "log"
    "math"
    "net/http"
    "os"
    "sync"
    "time"

    pxapi "github.com/Telmate/proxmox-api-go/proxmox"
    "github.com/fsnotify/fsnotify"
    "github.com/go-redis/redis/v8"
    "github.com/spf13/viper"
    "golang.org/x/sync/errgroup"
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
    redisClient   *redis.Client
    ctx           = context.Background()
    k8sClient     *kubernetes.Clientset
    proxmoxClient *pxapi.Client
    config        *viper.Viper
)

func main() {
    initConfig()
    initRedis()
    initKubernetesClient()
    initProxmoxClient()
    updateConfig() // Добавляем вызов функции для отслеживания изменений конфигурации

    http.HandleFunc("/status", statusHandler)
    http.HandleFunc("/healthz", healthzHandler)
    http.HandleFunc("/ready", readyHandler)
    
    server := &http.Server{
        Addr:         fmt.Sprintf(":%d", config.GetInt("server.port")),
        ReadTimeout:  config.GetDuration("server.readTimeout"),
        WriteTimeout: config.GetDuration("server.writeTimeout"),
        IdleTimeout:  config.GetDuration("server.idleTimeout"),
    }

    log.Printf("Server starting on %s", server.Addr)
    log.Fatal(server.ListenAndServe())
}

func initConfig() {
    config = viper.New()
    config.SetConfigName("config")
    config.SetConfigType("yaml")
    config.AddConfigPath("/etc/myapp/")
    err := config.ReadInConfig()
    if err != nil {
        log.Printf("Warning: Could not read config file: %s", err)
    }

    // Переопределение значений из переменных окружения
    config.SetEnvPrefix("MYAPP")
    config.AutomaticEnv()

    // Явное связывание ключей конфигурации с переменными окружения
    config.BindEnv("proxmox.apiUrl", "PROXMOX_API_URL")
    config.BindEnv("proxmox.username", "PROXMOX_USER")
    config.BindEnv("proxmox.password", "PROXMOX_PASSWORD")
    config.BindEnv("proxmox.insecureSkipVerify", "PROXMOX_INSECURE_SKIP_VERIFY")
    config.BindEnv("proxmox.taskTimeout", "PROXMOX_TASK_TIMEOUT")

    // Логирование значений конфигурации (будьте осторожны с конфиденциальными данными)
    log.Printf("Proxmox API URL: %s", config.GetString("proxmox.apiUrl"))
    log.Printf("Proxmox Username: %s", config.GetString("proxmox.username"))
    log.Printf("Proxmox InsecureSkipVerify: %v", config.GetBool("proxmox.insecureSkipVerify"))
    log.Printf("Proxmox Task Timeout: %v", config.GetDuration("proxmox.taskTimeout"))
}

func parseTimeout(timeoutStr string) (int, error) {
    timeoutStr = strings.TrimSpace(timeoutStr)
    if strings.HasSuffix(timeoutStr, "s") {
        timeoutStr = strings.TrimSuffix(timeoutStr, "s")
    }
    timeout, err := strconv.Atoi(timeoutStr)
    if err != nil {
        return 0, fmt.Errorf("invalid timeout value: %s", timeoutStr)
    }
    return timeout, nil
}

func initProxmoxClient() {
    tlsConfig := &tls.Config{InsecureSkipVerify: config.GetBool("proxmox.insecureSkipVerify")}
    
    log.Printf("Initializing Proxmox client")
    
    var err error
    proxmoxClient, err = pxapi.NewClient(
        config.GetString("proxmox.apiUrl"),
        nil,
        "",
        tlsConfig,
        "",
        0,
    )
    if err != nil {
        log.Fatalf("Failed to create Proxmox client: %v", err)
    }

    err = proxmoxClient.Login(config.GetString("proxmox.username"), config.GetString("proxmox.password"), "")
    if err != nil {
        log.Fatalf("Failed to login to Proxmox: %v", err)
    }

    log.Printf("Proxmox client initialized successfully")
}

func initRedis() {
    redisClient = redis.NewClient(&redis.Options{
        Addr:     config.GetString("redis.addr"),
        Password: config.GetString("redis.password"),
        DB:       config.GetInt("redis.db"),
        PoolSize: config.GetInt("redis.poolSize"),
    })

    _, err := redisClient.Ping(ctx).Result()
    if err != nil {
        log.Fatalf("Failed to connect to Redis: %v", err)
    }
    
    log.Println("Connected to Redis successfully")
}

func initKubernetesClient() {
    kubeconfig := config.GetString("kubernetes.config")
    config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
    if err != nil {
        log.Fatalf("Failed to build Kubernetes config: %v", err)
    }

    k8sClient, err = kubernetes.NewForConfig(config)
    if err != nil {
        log.Fatalf("Failed to create Kubernetes client: %v", err)
    }
}


func statusHandler(w http.ResponseWriter, r *http.Request) {
    ctx, cancel := context.WithTimeout(r.Context(), config.GetDuration("handler.timeout"))
    defer cancel()

    var (
        mu      sync.Mutex
        results = make(map[string]interface{})
        g, _    = errgroup.WithContext(ctx)
    )

    g.Go(func() error {
        helmReleasesRaw, err := getDataWithCache("helm_releases", getHelmReleases, config.GetDuration("cache.helmReleasesTTL"))
        if err != nil {
            return fmt.Errorf("failed to get Helm releases: %w", err)
        }
        mu.Lock()
        results["helmReleases"] = helmReleasesRaw
        mu.Unlock()
        return nil
    })

    g.Go(func() error {
        nodeStatusesRaw, err := getDataWithCache("node_statuses", getNodeStatuses, config.GetDuration("cache.nodeStatusesTTL"))
        if err != nil {
            return fmt.Errorf("failed to get node statuses: %w", err)
        }
        mu.Lock()
        results["nodeStatuses"] = nodeStatusesRaw
        mu.Unlock()
        return nil
    })

    g.Go(func() error {
        proxmoxNodesRaw, err := getDataWithCache("proxmox_nodes", getProxmoxNodes, config.GetDuration("cache.proxmoxNodesTTL"))
        if err != nil {
            return fmt.Errorf("failed to get Proxmox nodes: %w", err)
        }
        mu.Lock()
        results["proxmoxNodes"] = proxmoxNodesRaw
        mu.Unlock()
        return nil
    })

    g.Go(func() error {
        podStatusesRaw, err := getDataWithCache("pod_statuses", getPodStatuses, config.GetDuration("cache.podStatusesTTL"))
        if err != nil {
            return fmt.Errorf("failed to get pod statuses: %w", err)
        }
        mu.Lock()
        results["podStatuses"] = podStatusesRaw
        mu.Unlock()
        return nil
    })

    if err := g.Wait(); err != nil {
        http.Error(w, fmt.Sprintf("Internal server error: %v", err), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(results)
}

func getDataWithCache(cacheKey string, getData func() (interface{}, error), ttl time.Duration) (interface{}, error) {
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
    redisClient.Set(ctx, cacheKey, cacheData, ttl)

    return data, nil
}

func getHelmReleases() (interface{}, error) {
    // Используем уже инициализированный k8sClient
    namespaces, err := k8sClient.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
    if err != nil {
        return nil, fmt.Errorf("failed to list namespaces: %w", err)
    }

    var allReleases []HelmRelease
    var mu sync.Mutex
    var wg sync.WaitGroup

    for _, ns := range namespaces.Items {
        wg.Add(1)
        go func(namespace string) {
            defer wg.Done()
            settings := cli.New()
            actionConfig := new(action.Configuration)
            if err := actionConfig.Init(settings.RESTClientGetter(), namespace, os.Getenv("HELM_DRIVER"), log.Printf); err != nil {
                log.Printf("Failed to initialize action configuration for namespace %s: %v", namespace, err)
                return
            }

            listAction := action.NewList(actionConfig)
            releases, err := listAction.Run()
            if err != nil {
                log.Printf("Failed to list releases in namespace %s: %v", namespace, err)
                return
            }

            mu.Lock()
            defer mu.Unlock()
            for _, r := range releases {
                allReleases = append(allReleases, HelmRelease{
                    Name:      r.Name,
                    Namespace: r.Namespace,
                    Chart:     r.Chart.Metadata.Name,
                    Version:   r.Chart.Metadata.Version,
                    Status:    r.Info.Status.String(),
                })
            }
        }(ns.Name)
    }

    wg.Wait()
    return allReleases, nil
}

func getNodeStatuses() (interface{}, error) {
    nodes, err := k8sClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
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

func getProxmoxNodes() (interface{}, error) {
    nodeList, err := proxmoxClient.GetNodeList()
    if err != nil {
        return nil, fmt.Errorf("failed to get Proxmox nodes: %w", err)
    }

    var proxmoxNodes []ProxmoxNode
    for _, node := range nodeList["data"].([]interface{}) {
        n := node.(map[string]interface{})
        status := "unknown"
        if s, ok := n["status"].(string); ok {
            status = s
        }

        uptime := "unknown"
        if u, ok := n["uptime"].(float64); ok {
            uptime = formatUptime(time.Duration(u) * time.Second)
        }

        name, _ := n["node"].(string)

        proxmoxNodes = append(proxmoxNodes, ProxmoxNode{
            Name:   name,
            Status: status,
            Uptime: uptime,
        })
    }

    return proxmoxNodes, nil
}

func getPodStatuses() (interface{}, error) {
    pods, err := k8sClient.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})
    if err != nil {
        return nil, fmt.Errorf("failed to list pods: %w", err)
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

func healthzHandler(w http.ResponseWriter, r *http.Request) {
    // Проверка состояния всех зависимостей
    if err := checkRedis(); err != nil {
        http.Error(w, fmt.Sprintf("Redis unhealthy: %v", err), http.StatusServiceUnavailable)
        return
    }
    if err := checkKubernetes(); err != nil {
        http.Error(w, fmt.Sprintf("Kubernetes unhealthy: %v", err), http.StatusServiceUnavailable)
        return
    }
    if err := checkProxmox(); err != nil {
        http.Error(w, fmt.Sprintf("Proxmox unhealthy: %v", err), http.StatusServiceUnavailable)
        return
    }

    w.WriteHeader(http.StatusOK)
    w.Write([]byte("OK"))
}

func readyHandler(w http.ResponseWriter, r *http.Request) {
    // Проверка готовности приложения к обработке запросов
    if !isAppReady() {
        http.Error(w, "Application is not ready", http.StatusServiceUnavailable)
        return
    }

    w.WriteHeader(http.StatusOK)
    w.Write([]byte("Ready"))
}

func checkRedis() error {
    return redisClient.Ping(ctx).Err()
}

func checkKubernetes() error {
    _, err := k8sClient.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{Limit: 1})
    return err
}

func checkProxmox() error {
    _, err := proxmoxClient.GetNodeList()
    return err
}

func isAppReady() bool {
    // Здесь можно добавить дополнительные проверки готовности приложения
    return checkRedis() == nil && checkKubernetes() == nil && checkProxmox() == nil
}

// Функция для инвалидации кэша
func invalidateCache(key string) error {
    return redisClient.Del(ctx, key).Err()
}

// Функция для обновления конфигурации без перезапуска
func updateConfig() {
    config.WatchConfig()
    config.OnConfigChange(func(e fsnotify.Event) {
        log.Println("Config file changed:", e.Name)
        // Обновление настроек приложения
        updateRedisConfig()
        updateKubernetesConfig()
        updateProxmoxConfig()
    })
}

func updateRedisConfig() {
    // Обновление настроек Redis
    redisClient.Options().Addr = config.GetString("redis.addr")
    redisClient.Options().Password = config.GetString("redis.password")
    redisClient.Options().DB = config.GetInt("redis.db")
    redisClient.Options().PoolSize = config.GetInt("redis.poolSize")
}

func updateKubernetesConfig() {
    // Обновление настроек Kubernetes
    kubeconfig := config.GetString("kubernetes.config")
    newConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
    if err != nil {
        log.Printf("Failed to update Kubernetes config: %v", err)
        return
    }
    newClient, err := kubernetes.NewForConfig(newConfig)
    if err != nil {
        log.Printf("Failed to create new Kubernetes client: %v", err)
        return
    }
    k8sClient = newClient
}

func updateProxmoxConfig() {
    tlsConfig := &tls.Config{InsecureSkipVerify: config.GetBool("proxmox.insecureSkipVerify")}
    
    newClient, err := pxapi.NewClient(
        config.GetString("proxmox.apiUrl"),
        nil,
        "",
        tlsConfig,
        "",
        0,
    )
    if err != nil {
        log.Printf("Failed to create new Proxmox client: %v", err)
        return
    }
    err = newClient.Login(config.GetString("proxmox.username"), config.GetString("proxmox.password"), "")
    if err != nil {
        log.Printf("Failed to login to Proxmox with new config: %v", err)
        return
    }
    proxmoxClient = newClient
    log.Printf("Proxmox client updated successfully")
}

// Функция для логирования ошибок с дополнительным контекстом
func logError(err error, context string) {
    log.Printf("Error in %s: %v", context, err)
}

func init() {
    // Инициализация логгера или других глобальных настроек
    log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
}

// Главная функция main() уже определена выше