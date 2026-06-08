# Launchable rat-scheduler (cron) plugin image (ADR-022). Build from the REPO ROOT:
#   podman build -f plugins/scheduler/cron-py/Dockerfile -t rat/scheduler:dev .
FROM docker.io/library/python:3.12-slim
ENV PYTHONUNBUFFERED=1
WORKDIR /plugin
COPY plugins/scheduler/cron-py/ /plugin/
COPY contracts/sdks/python/rat /usr/local/lib/python3.12/site-packages/rat
RUN pip install --no-cache-dir -r requirements.txt
CMD ["python", "main.py"]
