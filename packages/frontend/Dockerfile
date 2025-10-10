FROM node:22-alpine

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm install

# Copy TypeScript config files first
COPY tsconfig.json tsconfig.app.json tsconfig.node.json ./

# Copy the rest of the application
COPY . .

EXPOSE 5173

CMD ["npm", "run", "dev", "--", "--host", "0.0.0.0"]