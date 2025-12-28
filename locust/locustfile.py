from locust import HttpUser, task, between
import random, time

class MetricUser(HttpUser):
    wait_time = between(0.001, 0.005)

    @task
    def send_metric(self):
        payload = {
            "device": f"device-{random.randint(1,200)}",
            "timestamp": int(time.time()),
            "cpu": round(random.random()*100,2),
            "rps": random.randint(50,150)
        }
        self.client.post("/ingest", json=payload)
