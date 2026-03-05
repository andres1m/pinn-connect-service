FROM python:3.11-slim

WORKDIR /app

# Копируем скрипт и веса модели (data.json)
COPY main.py .
COPY data.json .

# Создаем директории для ввода и вывода (их потом перекроют моунты из Docker Go-клиента)
RUN mkdir -p /app/input /app/result

# Переменные окружения, соответствующие логике скрипта
ENV DATA_DIR=/app
ENV INPUT_DIR=/app/input
ENV RESULT_DIR=/app/result

# Запуск скрипта
ENTRYPOINT ["python", "main.py"]
