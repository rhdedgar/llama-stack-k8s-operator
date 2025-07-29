# CA Bundle Configuration for LlamaStackDistribution

This document explains how to configure custom CA bundles for LlamaStackDistribution to enable secure communication with external LLM providers using self-signed certificates.

## Overview

The CA bundle configuration allows you to:
- Use self-signed certificates for external LLM API connections
- Trust custom Certificate Authorities (CAs) for secure communication
- Mount CA certificates from ConfigMaps into the LlamaStack server pods

## How It Works

When you configure a CA bundle:

1. **ConfigMap Storage**: CA certificates are stored in a Kubernetes ConfigMap
2. **Volume Mounting**: The certificates are mounted at `/etc/ssl/certs/` in the container
3. **Environment Variable**: The `SSL_CERT_FILE` environment variable is set to point to the CA bundle
4. **Automatic Restarts**: Pods restart automatically when the CA bundle ConfigMap changes

### Single Key vs Multiple Keys

**Single Key (configMapKey):**
- Direct ConfigMap volume mount
- Certificate file mounted directly from the ConfigMap key
- Minimal resource overhead

**Multiple Keys (configMapKeys):**
- Uses an InitContainer to concatenate multiple keys
- All certificates from specified keys are combined into a single file
- Slightly higher resource overhead due to InitContainer, but maintains standard SSL behavior
- The final consolidated file is always named `ca-bundle.crt` regardless of source key names

## Configuration Options

### Basic CA Bundle Configuration

```yaml
apiVersion: llamastack.io/v1alpha1
kind: LlamaStackDistribution
metadata:
  name: my-llama-stack
spec:
  server:
    distribution:
      name: hf-serverless
    tlsConfig:
      caBundle:
        configMapName: my-ca-bundle
        # configMapNamespace: default  # Optional - defaults to CR namespace
        # configMapKey: ca-bundle.crt           # Optional - defaults to "ca-bundle.crt"
```

### Multiple CA Bundle Keys Configuration (RHOAI Pattern)

```yaml
apiVersion: llamastack.io/v1alpha1
kind: LlamaStackDistribution
metadata:
  name: my-llama-stack
spec:
  server:
    distribution:
      name: hf-serverless
    tlsConfig:
      caBundle:
        configMapName: odh-trusted-ca-bundle
        # configMapNamespace: default  # Optional - defaults to CR namespace
        configMapKeys:                   # Multiple keys from same ConfigMap
          - ca-bundle.crt                # CNO-injected cluster CAs
          - odh-ca-bundle.crt           # User-specified custom CAs
```

### Configuration Fields

- `configMapName` (required): Name of the ConfigMap containing CA certificates
- `configMapNamespace` (optional): Namespace of the ConfigMap. Defaults to the same namespace as the LlamaStackDistribution
- `configMapKeys` (optional): Array of keys within the ConfigMap containing CA bundle data. All certificates from these keys will be concatenated into a single CA bundle file. If not specified, defaults to `["ca-bundle.crt"]`

## Examples

### Example 1: Basic CA Bundle

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-ca-bundle
data:
  ca-bundle.crt: |
    -----BEGIN CERTIFICATE-----
    # ... your CA certificate data here ...
    -----END CERTIFICATE-----
---
apiVersion: llamastack.io/v1alpha1
kind: LlamaStackDistribution
metadata:
  name: secure-llama-stack
spec:
  server:
    distribution:
      name: hf-serverless
    tlsConfig:
      caBundle:
        configMapName: my-ca-bundle
```

### Example 2: Custom Key Name

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-ca-bundle
data:
  custom-ca.pem: |
    -----BEGIN CERTIFICATE-----
    # ... your CA certificate data here ...
    -----END CERTIFICATE-----
---
apiVersion: llamastack.io/v1alpha1
kind: LlamaStackDistribution
metadata:
  name: secure-llama-stack
spec:
  server:
    distribution:
      name: hf-serverless
    tlsConfig:
      caBundle:
        configMapName: my-ca-bundle
        configMapKey: custom-ca.pem
```

### Example 3: Cross-Namespace CA Bundle

```yaml
apiVersion: llamastack.io/v1alpha1
kind: LlamaStackDistribution
metadata:
  name: secure-llama-stack
  namespace: my-namespace
spec:
  server:
    distribution:
      name: hf-serverless
    tlsConfig:
      caBundle:
        configMapName: global-ca-bundle
        configMapNamespace: kube-system
```

### Example 4: RHOAI Pattern with Multiple CA Sources

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: odh-trusted-ca-bundle
  labels:
    config.openshift.io/inject-trusted-cabundle: "true"
data:
  ca-bundle.crt: |
    # Populated by Cluster Network Operator (CNO)
    -----BEGIN CERTIFICATE-----
    # ... cluster-wide CA certificates ...
    -----END CERTIFICATE-----
  odh-ca-bundle.crt: |
    # User-specified custom CAs from DSCInitialization
    -----BEGIN CERTIFICATE-----
    # ... custom CA certificate 1 ...
    -----END CERTIFICATE-----
    -----BEGIN CERTIFICATE-----
    # ... custom CA certificate 2 ...
    -----END CERTIFICATE-----
---
apiVersion: llamastack.io/v1alpha1
kind: LlamaStackDistribution
metadata:
  name: rhoai-llama-stack
spec:
  server:
    distribution:
      name: hf-serverless
    tlsConfig:
      caBundle:
        configMapName: odh-trusted-ca-bundle
        configMapKeys:
          - ca-bundle.crt      # Cluster CAs
          - odh-ca-bundle.crt  # Custom CAs
```

## Creating CA Bundle ConfigMaps

### From Certificate Files

```bash
# Create a ConfigMap from a certificate file
kubectl create configmap my-ca-bundle --from-file=ca-bundle.crt=/path/to/your/ca.crt

# Or create from multiple certificate files
kubectl create configmap my-ca-bundle \
  --from-file=ca-bundle.crt=/path/to/your/ca1.crt \
  --from-file=additional-ca.crt=/path/to/your/ca2.crt
```

### From Certificate Content

```bash
# Create a ConfigMap with certificate content
kubectl create configmap my-ca-bundle --from-literal=ca-bundle.crt="$(cat /path/to/your/ca.crt)"
```

## Use Cases

### 1. Private Cloud Providers

When using private cloud LLM providers with self-signed certificates:

```yaml
spec:
  server:
    distribution:
      name: hf-serverless
    containerSpec:
      env:
      - name: HF_API_KEY
        valueFrom:
          secretKeyRef:
            name: hf-api-key
            key: token
    userConfig:
      configMapName: llama-stack-config
    tlsConfig:
      caBundle:
        configMapName: private-cloud-ca-bundle
```

### 2. Internal Enterprise APIs

For enterprise environments with internal CAs:

```yaml
spec:
  server:
    distribution:
      name: hf-endpoint
    tlsConfig:
      caBundle:
        configMapName: enterprise-ca-bundle
        configMapNamespace: security-system
```

### 3. Development/Testing

For development environments with self-signed certificates:

```yaml
spec:
  server:
    distribution:
      name: ollama
    tlsConfig:
      caBundle:
        configMapName: dev-ca-bundle
        configMapKey: development-ca.pem
```

## Troubleshooting

### Common Issues

1. **Certificate Not Found**: Ensure the ConfigMap exists and contains the specified key
2. **Permission Denied**: Check that the operator has permissions to read the ConfigMap
3. **Invalid Certificate**: Verify the certificate format is correct (PEM format)
4. **Pod Not Restarting**: ConfigMap changes trigger automatic pod restarts via annotations

### Common Error Messages and Solutions

#### "CA bundle key not found in ConfigMap"
- **Cause**: The specified key doesn't exist in the ConfigMap data
- **Solution**: Check the key name in your LlamaStackDistribution spec, default is "ca-bundle.crt"
- **Example**: Verify `kubectl get configmap my-ca-bundle -o yaml` shows your expected key

#### "Invalid CA bundle format"
- **Cause**: The certificate data is not in valid PEM format or contains invalid certificates
- **Solution**: Ensure certificates are properly formatted with BEGIN/END CERTIFICATE blocks
- **Example**: Valid format starts with `-----BEGIN CERTIFICATE-----`

#### "Referenced CA bundle ConfigMap not found"
- **Cause**: The ConfigMap specified in tlsConfig.caBundle.configMapName doesn't exist
- **Solution**: Create the ConfigMap first, then apply the LlamaStackDistribution
- **Example**: `kubectl create configmap my-ca-bundle --from-file=ca-bundle.crt=my-ca.crt`

#### "No valid certificates found in CA bundle"
- **Cause**: The ConfigMap contains data but no parseable certificates
- **Solution**: Verify certificate content and format
- **Example**: Use `openssl x509 -text -noout -in your-cert.crt` to validate certificates

#### "Failed to parse certificate"
- **Cause**: Certificate data is corrupted or not a valid X.509 certificate
- **Solution**: Regenerate the certificate or verify the source
- **Example**: Check if the certificate was properly base64 encoded

### Debugging

```bash
# Check if ConfigMap exists
kubectl get configmap my-ca-bundle -o yaml

# Check pod environment variables
kubectl describe pod <llama-stack-pod-name>

# Check mounted certificates
kubectl exec <llama-stack-pod-name> -- ls -la /etc/ssl/certs/

# Check SSL_CERT_FILE environment variable
kubectl exec <llama-stack-pod-name> -- env | grep SSL_CERT_FILE

# Validate certificate format locally
openssl x509 -text -noout -in ca-bundle.crt

# Check certificate expiration
openssl x509 -enddate -noout -in ca-bundle.crt

# Test certificate chain
openssl verify -CAfile ca-bundle.crt server.crt
```

### Validation Checklist

Before deploying a LlamaStackDistribution with CA bundle:

- [ ] ConfigMap exists in the correct namespace
- [ ] ConfigMap contains the specified key (default: "ca-bundle.crt")
- [ ] Certificate data is in PEM format
- [ ] Certificate data contains valid X.509 certificates
- [ ] Operator has read permissions on the ConfigMap
- [ ] Certificate is not expired
- [ ] Certificate contains the expected CA for your external service

## Security Considerations

1. **ConfigMap Security**: ConfigMaps are stored in plain text in etcd. Consider using appropriate RBAC policies
2. **Certificate Rotation**: Update ConfigMaps when certificates expire or are rotated
3. **Namespace Isolation**: Use appropriate namespaces to isolate CA bundles
4. **Audit Trail**: Monitor ConfigMap changes in production environments
5. **Principle of Least Privilege**: Only grant necessary permissions to access CA bundle ConfigMaps

## Limitations

- Only supports PEM format certificates
- ConfigMap size limits apply (1MB by default)
- Certificate validation is handled by the underlying Python SSL libraries
- Cross-namespace ConfigMap access requires appropriate RBAC permissions
