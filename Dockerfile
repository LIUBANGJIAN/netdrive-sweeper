FROM python:3.10-alpine AS builder

WORKDIR /app

COPY requirements.txt .

RUN pip install --no-cache-dir -r requirements.txt

FROM python:3.10-alpine

WORKDIR /app

COPY --from=builder /usr/local/lib/python3.10/site-packages /usr/local/lib/python3.10/site-packages
COPY --from=builder /usr/local/bin /usr/local/bin

COPY app.py .

RUN mkdir -p /app/data /CloudNAS && \
    rm -rf /var/cache/apk/*

VOLUME ["/app/data", "/CloudNAS"]

ENV CONFIG_PATH=/app/data/config.json
ENV CACHE_PATH=/app/data/cache.json
ENV LOG_PATH=/app/data/clean.log

EXPOSE 5000

CMD ["python", "app.py"]