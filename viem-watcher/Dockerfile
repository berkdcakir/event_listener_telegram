FROM node:20-alpine

WORKDIR /app

# package.json ve package-lock.json kopyala
COPY package*.json ./

# Prod bağımlılıkları yükle
RUN npm ci --only=production

# Kaynak kodları kopyala
COPY src ./src

ENV NODE_ENV=production

CMD ["node", "src/index.js"]

