FROM python:3.11-slim

# System dependencies for Camoufox (Firefox) + Xvfb + xdotool
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    tini \
    xvfb \
    xdotool \
    libgtk-3-0 \
    libx11-xcb1 \
    libasound2 \
    libdbus-glib-1-2 \
    libxt6 \
    libxcomposite1 \
    libxdamage1 \
    libxrandr2 \
    libpango-1.0-0 \
    libcairo2 \
    libatk1.0-0 \
    libcups2 \
    fonts-liberation \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

# Download Camoufox browser binary
RUN python -m camoufox fetch

COPY server.py context_api.py ./
COPY entrypoint.sh ./
RUN chmod +x entrypoint.sh

ENV WORKERS=1 REUSE_BROWSER=1 PORT=3099

EXPOSE 3099

HEALTHCHECK --interval=30s --timeout=10s --retries=3 \
    CMD curl -sf http://localhost:${PORT:-3099}/health || exit 1

ENTRYPOINT ["tini", "--", "./entrypoint.sh"]
