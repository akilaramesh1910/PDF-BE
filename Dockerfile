FROM golang:1.22-bookworm AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o main .

FROM debian:bookworm-slim
# Install necessary tools for document conversion
RUN apt-get update && apt-get install -y \
    libreoffice-writer \
    libreoffice-calc \
    libreoffice-impress \
    libreoffice-draw \
    libreoffice-pdfimport \
    default-jre-headless \
    fonts-dejavu \
    fonts-liberation \
    poppler-utils \
    imagemagick \
    pandoc \
    ghostscript \
    qpdf \
    && rm -rf /var/lib/apt/lists/*

# Fix ImageMagick policy to allow PDF operations
RUN sed -i 's/rights="none" pattern="PDF"/rights="read|write" pattern="PDF"/' /etc/ImageMagick-6/policy.xml

WORKDIR /app
COPY --from=builder /app/main .
# Create tmp directory for conversions
RUN mkdir -p tmp && chmod 777 tmp

EXPOSE 8080
CMD ["./main"]
