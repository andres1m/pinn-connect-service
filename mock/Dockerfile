FROM python:3.11-slim

WORKDIR /app

COPY main.py .
COPY data.json .

RUN mkdir -p /app/input /app/result

ENV DATA_DIR=/app
ENV INPUT_DIR=/app/input
ENV RESULT_DIR=/app/result

# Запуск скрипта
ENTRYPOINT ["python", "main.py"]
