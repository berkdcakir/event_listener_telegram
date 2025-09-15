FROM node:20-alpine
WORKDIR /app

# Sadece bağımlılık dosyalarını kopyala ve kur (build context repo kökü)
COPY viem-watcher/package*.json ./
RUN npm ci --only=production

# Uygulama kaynaklarını kopyala
COPY viem-watcher/src ./src

ENV NODE_ENV=production

CMD ["node", "src/index.js"]


