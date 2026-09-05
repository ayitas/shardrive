FROM node:24-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run check && npm run build

FROM node:24-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production
ENV HOST=0.0.0.0
ENV PORT=3000
COPY --from=build /app/package.json /app/package-lock.json ./
COPY --from=build /app/build ./build
COPY --from=build /app/node_modules ./node_modules
EXPOSE 3000
CMD ["node", "build"]
