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
        # key: ca-bundle.crt           # Optional - defaults to "ca-bundle.crt"
```

### Configuration Fields

- `configMapName` (required): Name of the ConfigMap containing CA certificates
- `configMapNamespace` (optional): Namespace of the ConfigMap. Defaults to the same namespace as the LlamaStackDistribution
- `key` (optional): Key within the ConfigMap containing the CA bundle data. Defaults to "ca-bundle.crt"

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
    MIIDQTCCAimgAwIBAgITBmyfz5m/jAo54vB4ikPmljZbyjANBgkqhkiG9w0BAQsF
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
        key: custom-ca.pem
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
        key: development-ca.pem
```

## Troubleshooting

### Common Issues

1. **Certificate Not Found**: Ensure the ConfigMap exists and contains the specified key
2. **Permission Denied**: Check that the operator has permissions to read the ConfigMap
3. **Invalid Certificate**: Verify the certificate format is correct (PEM format)
4. **Pod Not Restarting**: ConfigMap changes trigger automatic pod restarts via annotations

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
```

## Security Considerations

1. **ConfigMap Security**: ConfigMaps are stored in plain text in etcd. Consider using appropriate RBAC policies
2. **Certificate Rotation**: Update ConfigMaps when certificates expire or are rotated
3. **Namespace Isolation**: Use appropriate namespaces to isolate CA bundles
4. **Principle of Least Privilege**: Only include necessary CA certificates in the bundle

## Limitations

- Only supports PEM format certificates
- ConfigMap size limits apply (1MB by default)
- Certificate validation is handled by the underlying Python SSL libraries
- Cross-namespace ConfigMap access requires appropriate RBAC permissions 