FROM python:3.10-slim

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY app.py .

RUN mkdir -p /app/data /CloudNAS

VOLUME ["/app/data", "/CloudNAS"]

ENV CONFIG_PATH=/app/data/config.json
ENV CACHE_PATH=/app/data/cache.json
ENV LOG_PATH=/app/data/clean.log

EXPOSE 5000

CMD ["python", "app.py"]