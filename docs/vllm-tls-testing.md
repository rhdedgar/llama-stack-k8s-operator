# vLLM TLS Testing Guide

This document describes the TLS testing setup for the LlamaStack Kubernetes Operator that validates secure communication between LlamaStackDistribution and vLLM server using TLS certificates.

## Overview

The TLS testing infrastructure ensures that:
1. vLLM server can be deployed with TLS certificates
2. LlamaStackDistribution can securely connect to vLLM using a Certificate Authority (CA) bundle
3. The entire TLS certificate chain is validated
4. The connection is secure and functional

## Components

### 1. Certificate Generation

The `config/samples/generate_certificates.sh` script generates:
- **CA Private Key** (`ca.key`): Certificate Authority private key
- **CA Certificate** (`ca.crt`): Self-signed CA certificate
- **Server Private Key** (`server.key`): vLLM server private key
- **Server Certificate** (`server.crt`): vLLM server certificate signed by CA
- **CA Bundle** (`ca-bundle.crt`): Combined server certificate and CA certificate

The certificates are organized into:
- `vllm-certs/`: Contains server certificate and key for vLLM deployment
- `vllm-ca-certs/`: Contains CA bundle for LlamaStackDistribution

### 2. vLLM Deployment Configuration

**File**: `config/samples/vllm-k8s.yaml`

This configuration deploys a Kubernetes-compatible vLLM server with:
- TLS enabled using generated certificates
- Proper resource limits for CI environments
- CPU-only configuration for GitHub Actions
- Placeholder HuggingFace token for testing

Key features:
- Namespace: `vllm-dist`
- Service: `vllm-server` on port 8000
- TLS certificates mounted at `/etc/ssl/certs`
- Environment variables for CPU-only inference

### 3. LlamaStackDistribution Configuration

**File**: `config/samples/vllm-tls-test.yaml`

This configuration creates a LlamaStackDistribution that:
- Connects to vLLM server using HTTPS
- Uses the CA bundle for certificate validation
- Configured with `remote-vllm` distribution
- Includes proper environment variables for vLLM URL

## Testing Workflow

### GitHub Actions Integration

The e2e tests are integrated into the GitHub Actions workflow (`.github/workflows/run-e2e-test.yml`):

1. **Certificate Generation**: 
   ```bash
   ./config/samples/generate_certificates.sh
   ```

2. **Secret Creation**:
   ```bash
   kubectl create secret generic vllm-ca-certs --from-file=./config/samples/vllm-ca-certs/
   kubectl create secret generic vllm-certs --from-file=./config/samples/vllm-certs/
   ```

3. **vLLM Deployment**:
   ```bash
   kubectl apply -f config/samples/vllm-k8s.yaml
   kubectl wait --for=condition=available --timeout=600s deployment/vllm-server -n vllm-dist
   ```

4. **E2E Test Execution**: Runs the complete test suite including vLLM TLS tests

### E2E Test Suite

**File**: `tests/e2e/vllm_tls_test.go`

The test suite includes:

1. **Setup Phase**:
   - Creates namespaces (`vllm-dist`, `llama-stack-test`)
   - Copies TLS certificates to appropriate namespaces
   - Creates CA bundle ConfigMaps

2. **vLLM Deployment Phase**:
   - Deploys vLLM server with TLS configuration
   - Waits for deployment and service readiness
   - Validates pod health

3. **TLS Validation Phase**:
   - Validates certificate files exist and are readable
   - Checks vLLM pod readiness
   - Validates TLS configuration

4. **LlamaStack Integration Phase**:
   - Deploys LlamaStackDistribution with CA bundle
   - Waits for LlamaStack deployment readiness
   - Validates provider health status

5. **Connection Validation Phase**:
   - Checks LlamaStackDistribution status
   - Validates vLLM provider is healthy
   - Ensures secure connection is established

6. **Cleanup Phase**:
   - Removes test resources
   - Validates proper cleanup

## Certificate Details

### CA Certificate
- **Subject**: `/C=US/ST=California/L=Los Angeles/O=Demo Corp/OU=OpenShift CA/CN=example-ca`
- **Validity**: 3650 days (10 years)
- **Key Size**: 2048 bits RSA

### Server Certificate
- **Subject**: `/C=US/ST=California/L=Los Angeles/O=Demo Corp/OU=OpenShift Service/CN=vllm-server.vllm-dist.svc.cluster.local`
- **Validity**: 365 days (1 year)
- **Key Size**: 2048 bits RSA
- **DNS Name**: `vllm-server.vllm-dist.svc.cluster.local`

## Security Considerations

1. **Test Environment Only**: The generated certificates are for testing purposes only
2. **Self-Signed CA**: Uses a self-signed CA which is not suitable for production
3. **Placeholder Tokens**: Uses dummy HuggingFace tokens for testing
4. **Resource Limits**: Configured with minimal resources for CI environments

## Troubleshooting

### Common Issues

1. **Certificate Not Found**: Ensure `generate_certificates.sh` runs successfully
2. **vLLM Pod Not Ready**: Check resource limits and CPU-only configuration
3. **TLS Connection Failed**: Verify CA bundle is correctly mounted in LlamaStackDistribution
4. **Provider Health Check Failed**: Ensure vLLM service is accessible and healthy

### Debug Commands

```bash
# Check certificate generation
ls -la config/samples/vllm-certs/
ls -la config/samples/vllm-ca-certs/

# Check vLLM deployment
kubectl get pods -n vllm-dist
kubectl logs -n vllm-dist deployment/vllm-server

# Check LlamaStack deployment
kubectl get pods -n llama-stack-test
kubectl logs -n llama-stack-test deployment/vllm-tls-test

# Check certificate in pod
kubectl exec -n vllm-dist deployment/vllm-server -- ls -la /etc/ssl/certs/
```

## Future Enhancements

1. **Real TLS Testing**: Implement actual HTTPS requests to vLLM server
2. **Certificate Rotation**: Test certificate rotation scenarios
3. **Multiple CA Support**: Test with multiple certificate authorities
4. **Production Certificates**: Integration with cert-manager or similar tools
5. **Mutual TLS**: Implement client certificate authentication

## Related Files

- `config/samples/generate_certificates.sh`: Certificate generation script
- `config/samples/vllm-k8s.yaml`: Kubernetes vLLM deployment
- `config/samples/vllm-tls-test.yaml`: LlamaStackDistribution TLS configuration
- `tests/e2e/vllm_tls_test.go`: E2E test suite
- `.github/workflows/run-e2e-test.yml`: GitHub Actions workflow 