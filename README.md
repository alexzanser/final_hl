# Simple Go Streaming Service (metrics ingest + analytics)

Коротко: проект реализует HTTP-сервис на Go для приёма JSON-метрик, кэширует их в Redis, выполняет простую аналитику (rolling average, z-score) и экспортирует метрики в Prometheus.

Быстрый старт локально (Minikube):

1. Собрать образ локально:

```bash
docker build -t go-service:latest .
```

2. Запустить Minikube (пример):

```bash
minikube start --cpus=2 --memory=4096
kubectl apply -f k8s/redis.yaml
kubectl apply -f k8s/deployment.yaml
kubectl apply -f k8s/service.yaml
kubectl apply -f k8s/hpa.yaml
kubectl apply -f k8s/ingress.yaml
kubectl apply -f prometheus/
kubectl apply -f grafana/
```

3. Тестирование с Locust (локально):

```bash
locust -f locust/locustfile.py --headless -u 200 -r 20 --run-time 5m --host=http://$(minikube ip)
```

Файлы в проекте:
- `main.go` — основной код сервиса
- `Dockerfile` — сборка образа
- `k8s/` — манифесты Kubernetes (Deployment, Service, HPA, Redis, Ingress)
- `prometheus/` — минимальная конфигурация Prometheus
- `grafana/` — минимальная конфигурация Grafana
- `locust/locustfile.py` — скрипт для нагрузочного теста
