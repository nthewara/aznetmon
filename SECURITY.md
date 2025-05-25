# Security Policy

## Supported Versions

We actively support the following versions of AzNetMon:

| Version | Supported          |
| ------- | ------------------ |
| 1.0.x   | :white_check_mark: |
| < 1.0   | :x:                |

## Reporting a Vulnerability

We take security seriously. If you discover a security vulnerability, please follow these steps:

### 1. Do NOT create a public GitHub issue

Security vulnerabilities should be reported privately to protect users.

### 2. Report via GitHub Security Advisories

1. Go to the [Security tab](https://github.com/nirmalchrist/aznetmon/security) of this repository
2. Click "Report a vulnerability"
3. Provide detailed information about the vulnerability

### 3. What to include in your report

- **Description**: Clear description of the vulnerability
- **Impact**: What an attacker could achieve
- **Steps to reproduce**: Detailed steps to reproduce the issue
- **Environment**: Operating system, Go version, Docker version (if applicable)
- **Proof of concept**: Code or commands that demonstrate the vulnerability

### 4. Response timeline

- **Initial response**: Within 48 hours
- **Status update**: Within 1 week
- **Fix timeline**: Depends on severity, typically within 2-4 weeks

### 5. Responsible disclosure

We request that you:
- Give us reasonable time to fix the vulnerability before public disclosure
- Do not access or modify data that doesn't belong to you
- Do not perform testing that could harm our users or infrastructure

## Security considerations

### Running with elevated privileges

AzNetMon requires `NET_RAW` capability or `sudo` privileges to send ICMP packets. This is a necessary requirement for ping functionality.

**Recommendations:**
- Use the provided Docker container with `--cap-add=NET_RAW` (preferred)
- If running natively, use `setcap cap_net_raw=ep ./aznetmon` instead of `sudo`
- Never run as root in production unless absolutely necessary

### Network security

- AzNetMon only sends ICMP packets to targets you specify
- No data is collected or transmitted to external services
- All monitoring data stays on your local system
- WebSocket connections use same-origin policy in production

### Container security

The Docker container follows security best practices:
- Runs as non-root user (uid 1000)
- Uses minimal Alpine Linux base image
- No unnecessary packages or tools included
- Health checks included for monitoring

## Known limitations

- ICMP packets can reveal your server's IP to monitored targets
- WebSocket connections in development mode allow all origins (production should use proper CORS)
- No built-in authentication (designed for internal networks)

## Security updates

Security updates will be released as patch versions and announced via:
- GitHub Security Advisories
- GitHub Releases with security tags
- CHANGELOG.md with security notes

Thank you for helping keep AzNetMon secure!
