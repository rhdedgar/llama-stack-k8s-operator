# CA Bundle Configuration for LlamaStackDistribution

This document explains how to configure custom CA bundles for LlamaStackDistribution to enable secure communication with external LLM providers using self-signed certificates.

## Overview

The CA bundle configuration allows you to:
- Use self-signed certificates for external LLM API connections
- Trust custom Certificate Authorities (CAs) for secure communication
- Mount CA certificates from ConfigMaps into the LlamaStack server pods
- **NEW**: Automatically discover and use CA bundles from OpenDataHub trusted CA bundle ConfigMap

## Auto-Discovery from OpenDataHub Trusted CA Bundle

LlamaStackDistribution can automatically discover and use custom CA bundles from OpenDataHub's trusted CA bundle ConfigMap. This feature is designed for environments where the OpenDataHub Operator is deployed and manages cluster-wide trusted CA bundles.

### How Auto-Discovery Works

1. **Detection**: The operator checks for the `odh-trusted-ca-bundle` ConfigMap in the same namespace as the LlamaStackDistribution
2. **Direct Mounting**: If found, it mounts the CA bundle directly at `/etc/ssl/certs/odh-ca-bundle.crt` and sets SSL environment variables
3. **No Duplication**: The operator does not create copies - it uses the ConfigMap provided by the OpenShift AI operator

### When Auto-Discovery is Used

Auto-discovery is automatically enabled when:
- No explicit `tlsConfig.caBundle` is configured in the LlamaStackDistribution
- The `odh-trusted-ca-bundle` ConfigMap exists in the same namespace
- The ConfigMap contains valid CA bundle data in either `odh-ca-bundle.crt` or `ca-bundle.crt` keys

### Example: LlamaStackDistribution with Auto-Discovery

```yaml
apiVersion: llamastack.io/v1alpha1
kind: LlamaStackDistribution
metadata:
  name: llamastack-with-auto-ca-bundle
spec:
  replicas: 1
  server:
    distribution:
      name: remote-vllm
    containerSpec:
      env:
      - name: VLLM_URL
        value: "https://vllm-server.vllm-dist.svc.cluster.local:8000/v1"
      - name: VLLM_TLS_VERIFY
        value: "/etc/ssl/certs/odh-ca-bundle.crt"
    # No tlsConfig specified - auto-discovery will use the odh-trusted-ca-bundle
    # ConfigMap if it exists in the same namespace
```

### OpenDataHub ConfigMap Distribution

The OpenShift AI operator automatically copies the `odh-trusted-ca-bundle` ConfigMap to all namespaces, so it should be available in the same namespace as your LlamaStackDistribution:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: odh-trusted-ca-bundle
  namespace: my-namespace  # Same namespace as LlamaStackDistribution
data:
  odh-ca-bundle.crt: |
    -----BEGIN CERTIFICATE-----
    # ... your custom CA certificate data here ...
    -----END CERTIFICATE-----
  ca-bundle.crt: |
    -----BEGIN CERTIFICATE-----
    # ... alternative or additional CA certificate data ...
    -----END CERTIFICATE-----
```

### Automatic Cleanup

The simplified approach eliminates the need for complex cleanup logic:

- **No Local Copies**: The operator mounts the ConfigMap directly without creating duplicates
- **OpenShift AI Management**: The OpenShift AI operator handles ConfigMap lifecycle
- **No Orphaned Resources**: Since no local copies are created, there are no orphaned ConfigMaps to clean up

### Priority Order

The CA bundle configuration follows this priority order:

1. **Explicit Configuration**: If `tlsConfig.caBundle` is specified, it takes precedence
2. **ODH Auto-Discovery**: If no explicit config and ODH trusted CA bundle ConfigMap exists with valid data
3. **None**: No CA bundle configuration

### Troubleshooting Auto-Discovery

#### Auto-Discovery Not Working

1. **Check ODH trusted CA bundle ConfigMap existence in same namespace**:
   ```bash
   kubectl get configmap odh-trusted-ca-bundle -n <namespace>
   ```

2. **Verify CA bundle data keys**:
   ```bash
   kubectl get configmap odh-trusted-ca-bundle -n <namespace> -o jsonpath='{.data}' | jq 'keys'
   ```

3. **Check operator logs**:
   ```bash
   kubectl logs -n llama-stack-operator-system deployment/llama-stack-operator-controller-manager
   ```

4. **Verify OpenShift AI operator is copying ConfigMaps**:
   ```bash
   # Check if ConfigMap exists in multiple namespaces
   kubectl get configmap odh-trusted-ca-bundle --all-namespaces
   ```

5. **Check pod volume mounts**:
   ```bash
   kubectl describe pod -l app.kubernetes.io/instance=<llamastack-name> -n <namespace>
   ```

#### Common Auto-Discovery Issues

- **ODH ConfigMap not found in namespace**: Ensure OpenShift AI Operator is installed and functioning
- **Missing CA bundle keys**: Check that the ConfigMap has either `odh-ca-bundle.crt` or `ca-bundle.crt` keys
- **Empty CA bundle data**: Verify the ConfigMap contains valid CA certificate data
- **OpenShift AI operator not running**: Verify the OpenShift AI operator is properly deployed and copying ConfigMaps
- **Pod mount failures**: Check if the ConfigMap volume mount is optional and not causing pod startup failures

#### Disabling Auto-Discovery

To disable auto-discovery for a specific LlamaStackDistribution, simply configure an explicit CA bundle:

```yaml
spec:
  server:
    tlsConfig:
      caBundle:
        configMapName: my-custom-ca-bundle
```

Or set an empty/dummy CA bundle configuration:

```yaml
spec:
  server:
    tlsConfig:
      caBundle:
        configMapName: "disabled"  # This will prevent auto-discovery
```

## Manual CA Bundle Configuration

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
        # configMapKey: ca-bundle.crt           # Optional - defaults to "ca-bundle.crt"
```

### Configuration Fields

- `configMapName` (required): Name of the ConfigMap containing CA certificates
- `configMapNamespace` (optional): Namespace of the ConfigMap. Defaults to the same namespace as the LlamaStackDistribution
- `configMapKey` (optional): Key within the ConfigMap containing the CA bundle data. Defaults to "ca-bundle.crt"

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
