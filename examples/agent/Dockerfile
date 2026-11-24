FROM python:3.13-slim
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
WORKDIR /app
COPY agent.py /app/agent.py
USER 65534:65534
ENTRYPOINT ["python3", "-u", "/app/agent.py"]
