package main

import (
    "fmt"
    "log"
    "net/http"
    "os"
    "time"

    "github.com/spf13/viper"
)

var (
    config   *viper.Viper
    cacheTTL time.Duration
)

func main() {
    initConfig()
    initRedis()
    initKubernetesClient()
    initProxmoxClient()

    // Запускаем фоновое обновление кеша
    go updateCacheperiodically()

    http.HandleFunc("/status", statusHandler)
    http.HandleFunc("/healthz", healthzHandler)
    http.HandleFunc("/ready", readyHandler)

    log.Printf("Server starting on :%d", config.GetInt("server.port"))
    log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", config.GetInt("server.port")), nil))
}

func initConfig() {
    config = viper.New()
    config.SetConfigName("config")
    config.SetConfigType("yaml")
    config.AddConfigPath("/etc/myapp/")
    err := config.ReadInConfig()
    if err != nil {
        log.Fatalf("Fatal error config file: %s", err)
    }

    // Переопределение значений из переменных окружения
    config.SetEnvPrefix("MYAPP")
    config.AutomaticEnv()

    // Явное связывание ключей конфигурации с переменными окружения для Proxmox
    config.BindEnv("proxmox.apiUrl", "PROXMOX_API_URL")
    config.BindEnv("proxmox.username", "PROXMOX_USER")
    config.BindEnv("proxmox.password", "PROXMOX_PASSWORD")

    // Установка значений из переменных окружения
    if apiURL := os.Getenv("PROXMOX_API_URL"); apiURL != "" {
        config.Set("proxmox.apiUrl", apiURL)
    }
    if username := os.Getenv("PROXMOX_USER"); username != "" {
        config.Set("proxmox.username", username)
    }
    if password := os.Getenv("PROXMOX_PASSWORD"); password != "" {
        config.Set("proxmox.password", password)
    }

    cacheTTL = config.GetDuration("cache.ttl")
    if cacheTTL == 0 {
        cacheTTL = 10 * time.Second // Значение по умолчанию
    }

    // Логирование конфигурации (без вывода чувствительных данных)
    log.Printf("Proxmox API URL: %s", config.GetString("proxmox.apiUrl"))
    log.Printf("Proxmox Username: %s", config.GetString("proxmox.username"))
    log.Printf("Cache TTL: %s", cacheTTL)
}