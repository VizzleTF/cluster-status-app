package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "time"

    "helm.sh/helm/v3/pkg/action"
    "helm.sh/helm/v3/pkg/cli"
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/tools/clientcmd"
    "k8s.io/metrics/pkg/client/clientset/versioned"
)

var (
    k8sClient    *kubernetes.Clientset
    metricsClient *versioned.Clientset
)

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

    metricsClient, err = versioned.NewForConfig(config)
    if err != nil {
        log.Fatalf("Failed to create Kubernetes metrics client: %v", err)
    }
}

func getHelmReleases() ([]HelmRelease, error) {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    namespaces, err := k8sClient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
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
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    nodes, err := k8sClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
    if err != nil {
        return nil, fmt.Errorf("failed to list nodes: %w", err)
    }

    nodeMetrics, err := metricsClient.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{})
    if err != nil {
        return nil, fmt.Errorf("failed to get node metrics: %w", err)
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

        cpuCapacity := node.Status.Capacity.Cpu().MilliValue()
        memoryCapacity := node.Status.Capacity.Memory().Value()
        storageCapacity := node.Status.Capacity.StorageEphemeral().Value()

        cpuUsage := int64(0)
        memoryUsage := int64(0)
        for _, metric := range nodeMetrics.Items {
            if metric.Name == node.Name {
                cpuUsage = metric.Usage.Cpu().MilliValue()
                memoryUsage = metric.Usage.Memory().Value()
                break
            }
        }

        nodeStatus := NodeStatus{
            Name:       node.Name,
            Status:     status,
            Roles:      getRoles(node.Labels),
            Version:    node.Status.NodeInfo.KubeletVersion,
            InternalIP: internalIP,
            Age:        formatUptime(time.Since(node.CreationTimestamp.Time)),
            CPUUsage:   cpuUsage,
            CPULimit:   cpuCapacity,
            MemoryUsage: memoryUsage,
            MemoryLimit: memoryCapacity,
            StorageUsage: node.Status.Allocatable.StorageEphemeral().Value(),
            StorageLimit: storageCapacity,
        }

        nodeStatuses = append(nodeStatuses, nodeStatus)
    }

    return nodeStatuses, nil
}

func getPodStatuses() (PodStatuses, error) {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    pods, err := k8sClient.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
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

func checkKubernetes() error {
    _, err := k8sClient.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{Limit: 1})
    return err
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
        roles = append(roles, "worker")
    }
    return roles
}

func formatUptime(duration time.Duration) string {
    days := int(duration.Hours() / 24)
    hours := int(duration.Hours()) % 24
    minutes := int(duration.Minutes()) % 60
    return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
}