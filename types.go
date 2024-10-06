package main

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