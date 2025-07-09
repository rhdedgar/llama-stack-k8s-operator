#!/bin/bash

# --- Configuration ---

# CA Details
CA_KEY="ca.key"
CA_CERT="ca.crt"
CA_SUBJECT="/C=US/ST=California/L=Los Angeles/O=Demo Corp/OU=OpenShift CA/CN=example-ca"

# Server Certificate Details
SERVER_KEY="server.key"
SERVER_CSR="server.csr"
SERVER_CERT="server.crt"
SERVER_SUBJECT="/C=US/ST=California/L=Los Angeles/O=Demo Corp/OU=OpenShift Service/CN=vllm-server.vllm-dist.svc.cluster.local"

# Bundle
CA_BUNDLE="ca-bundle.crt"


# --- 1. Create the Certificate Authority (CA) ---

echo "Generating CA private key and self-signed certificate..."

# Generate the CA's private key
openssl genrsa -out "${CA_KEY}" 2048

# Generate the self-signed CA certificate
openssl req -x509 -new -nodes -key "${CA_KEY}" \
  -sha256 -days 3650 \
  -subj "${CA_SUBJECT}" \
  -out "${CA_CERT}"

echo "CA created successfully: ${CA_KEY}, ${CA_CERT}"


# --- 2. Create the Server Certificate and Sign with CA ---

echo "Generating server private key and certificate signing request (CSR)..."

# Generate the server's private key
openssl genrsa -out "${SERVER_KEY}" 2048

# Generate the server's CSR
openssl req -new -nodes -key "${SERVER_KEY}" \
  -subj "${SERVER_SUBJECT}" \
  -out "${SERVER_CSR}"

echo "Signing server certificate with the CA..."

# Sign the server CSR with the CA key to create the server certificate
openssl x509 -req -in "${SERVER_CSR}" \
  -CA "${CA_CERT}" -CAkey "${CA_KEY}" -CAcreateserial \
  -out "${SERVER_CERT}" -days 365 -sha256

echo "Server certificate created and signed: ${SERVER_CERT}"


# --- 3. Create the Final Certificate Bundle ---

echo "Creating the final certificate bundle..."

# Concatenate the server certificate and the CA certificate into a single bundle file
cat "${SERVER_CERT}" "${CA_CERT}" > "${CA_BUNDLE}"

echo "   All files created successfully!"
echo "------------------------------------"
echo "  - CA Private Key:           ${CA_KEY}"
echo "  - CA Certificate:           ${CA_CERT}"
echo "  - Server Private Key:       ${SERVER_KEY}"
echo "  - Server Certificate:       ${SERVER_CERT}"
echo "  - Combined Bundle:          ${CA_BUNDLE}"
echo "------------------------------------"

# Clean up the server's CSR as it is no longer needed
rm "${SERVER_CSR}"

mkdir vllm-certs

cp "${SERVER_CERT}" vllm-certs/
cp "${SERVER_KEY}" vllm-certs/

mkdir vllm-ca-certs

cp "${CA_BUNDLE}" vllm-ca-certs/
