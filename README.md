# Cluster Status App

Cluster Status App - это приложение для мониторинга состояния кластера Kubernetes и Proxmox. Оно предоставляет информацию о состоянии узлов, подов, Helm-релизов и Proxmox-узлов через удобный API.

## Особенности

- Мониторинг состояния узлов Kubernetes
- Отслеживание статусов подов
- Информация о Helm-релизах
- Мониторинг узлов Proxmox
- RESTful API для получения данных о состоянии кластера

## Требования

- Kubernetes кластер
- Helm 3
- Доступ к Proxmox API
- Go 1.16+ (для разработки)

## Установка

### Использование Helm

1. Добавьте Helm репозиторий:
   ```
   helm repo add cluster-status https://vizzletf.github.io/cluster-status
   helm repo update
   ```

2. Установите чарт:
   ```
   helm install cluster-status cluster-status/cluster-status-app --namespace cluster-status --create-namespace
   ```

### Настройка секретов

Перед установкой убедитесь, что у вас созданы необходимые секреты:

1. Создайте секрет с учетными данными Proxmox:
   ```
   kubectl create secret generic proxmox-credentials \
     --from-literal=api-url=https://your-proxmox-api-url \
     --from-literal=username=your-proxmox-user \
     --from-literal=password=your-proxmox-password \
     -n cluster-status
   ```

2. Создайте секрет с kubeconfig:
   ```
   kubectl create secret generic kubeconfig --from-file=config=/path/to/your/kubeconfig -n cluster-status
   ```

## Использование

После установки, приложение будет доступно по адресу, указанному в вашем Ingress. 

Для получения статуса кластера, отправьте GET-запрос на эндпоинт `/status`.

## Разработка

1. Клонируйте репозиторий:
   ```
   git clone https://github.com/vizzletf/cluster-status-app.git
   cd cluster-status-app
   ```

2. Установите зависимости:
   ```
   go mod download
   ```

3. Запустите приложение локально:
   ```
   go run main.go
   ```

## Структура проекта

```
cluster-status-app/
├── .github/            # GitHub Actions workflows
├── charts/             # Helm chart
├── Dockerfile          # Dockerfile для сборки образа
├── go.mod              # Go модули
├── go.sum              # Go модули checksums
├── main.go             # Main.go
└── README.md           # Этот файл
```

## Вклад в проект

Мы приветствуем вклад в развитие проекта! Пожалуйста, создайте issue или отправьте pull request с вашими предложениями.

## Лицензия

Этот проект распространяется под лицензией MIT. Смотрите файл [LICENSE](LICENSE) для получения дополнительной информации.