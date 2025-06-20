# Dockerfile for pds-one service (Node.js + Yarn)
FROM node:20-alpine
WORKDIR /app
COPY . .
RUN yarn install --frozen-lockfile
CMD ["yarn", "start"]

