package main

import (
    "crypto/tls"
    "fmt"
    "log"
    "time"
    "strings"

    pxapi "github.com/Telmate/proxmox-api-go/proxmox"
)

var proxmoxClient *pxapi.Client

func initProxmoxClient() {
    tlsConfig := &tls.Config{InsecureSkipVerify: config.GetBool("proxmox.insecureSkipVerify")}
    
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

    go func() {
        for {
            err := proxmoxClient.Login(config.GetString("proxmox.username"), config.GetString("proxmox.password"), "")
            if err != nil {
                log.Printf("Failed to login to Proxmox: %v", err)
            } else {
                log.Println("Successfully logged in to Proxmox")
            }
            time.Sleep(15 * time.Minute)
        }
    }()
    
    log.Println("Proxmox client initialized successfully")
}

func getProxmoxNodes() ([]ProxmoxNode, error) {
    nodeList, err := proxmoxClient.GetNodeList()
    if err != nil {
        if strings.Contains(err.Error(), "permission denied") {
            // Попытка повторной аутентификации
            loginErr := proxmoxClient.Login(config.GetString("proxmox.username"), config.GetString("proxmox.password"), "")
            if loginErr != nil {
                return nil, fmt.Errorf("failed to re-authenticate with Proxmox: %w", loginErr)
            }
            // Повторная попытка получения списка узлов
            nodeList, err = proxmoxClient.GetNodeList()
            if err != nil {
                return nil, fmt.Errorf("failed to get Proxmox nodes after re-authentication: %w", err)
            }
        } else {
            return nil, fmt.Errorf("failed to get Proxmox nodes: %w", err)
        }
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

func checkProxmox() error {
    _, err := proxmoxClient.GetNodeList()
    return err
}