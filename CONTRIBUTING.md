# Contributing to AzNetMon

Thank you for your interest in contributing to AzNetMon! We welcome contributions from everyone.

## Code of Conduct

By participating in this project, you agree to abide by our Code of Conduct. Please be respectful and inclusive.

## How to Contribute

### Reporting Bugs

1. Check if the issue already exists in the [Issues](https://github.com/nthewara/aznetmon/issues)
2. If not, create a new issue with:
   - Clear description of the problem
   - Steps to reproduce
   - Expected vs actual behavior
   - Environment details (OS, Go version, Docker version)
   - Screenshots if applicable

### Suggesting Features

1. Check existing [Issues](https://github.com/nthewara/aznetmon/issues) and [Discussions](https://github.com/nthewara/aznetmon/discussions)
2. Create a new issue with:
   - Clear description of the feature
   - Use case and motivation
   - Proposed implementation (if you have ideas)

### Pull Requests

1. **Fork** the repository
2. **Create a branch** from `main`:
   ```bash
   git checkout -b feature/your-feature-name
   ```
3. **Make your changes**:
   - Write clean, documented code
   - Follow existing code style
   - Add tests if applicable
   - Update documentation
4. **Test your changes**:
   ```bash
   make test
   make build
   make docker-build
   ```
5. **Commit** with clear messages:
   ```bash
   git commit -m "feat: add new monitoring feature"
   ```
6. **Push** to your fork:
   ```bash
   git push origin feature/your-feature-name
   ```
7. **Create a Pull Request**

## Development Setup

### Prerequisites
- Go 1.24 or higher
- Docker (for container testing)
- Git

### Dependencies
- github.com/gorilla/websocket v1.5.3
- golang.org/x/net v0.40.0
- Development tools:
  - github.com/cosmtrek/air (live reload)
  - github.com/securecodewarrior/gosec/v2 (security scanner)

### Local Development
```bash
# Clone your fork
git clone https://github.com/nthewara/aznetmon.git
cd aznetmon

# Install dependencies
go mod tidy

# Install development tools
make install-dev-tools

# Run tests
make test

# Run locally
make run

# Run with hot reload
make dev
```

### Testing
```bash
# Run all tests
make test

# Run with coverage
go test -cover ./...

# Test Docker build
make docker-build

# Test deployment script
./deploy.sh "8.8.8.8,1.1.1.1"
```

## Code Style

### Go Code
- Follow standard Go formatting (`go fmt`)
- Use meaningful variable and function names
- Add comments for public functions
- Keep functions small and focused
- Handle errors appropriately

### Commit Messages
Follow [Conventional Commits](https://www.conventionalcommits.org/):
- `feat:` new feature
- `fix:` bug fix
- `docs:` documentation changes
- `style:` formatting changes
- `refactor:` code refactoring
- `test:` adding tests
- `chore:` maintenance tasks

### Example:
```
feat: add support for IPv6 ping monitoring

- Add IPv6 address resolution
- Update UI to display IPv6 addresses
- Add tests for IPv6 functionality

Closes #123
```

## Areas for Contribution

### 🚀 High Priority
- [ ] IPv6 support
- [ ] Prometheus metrics endpoint
- [ ] Configuration file support
- [ ] Test coverage improvements

### 🎨 UI/UX
- [ ] Dark/light theme toggle
- [ ] Charts and graphs
- [ ] Mobile app responsiveness
- [ ] Accessibility improvements

### 🔧 Features
- [ ] Email/Slack notifications
- [ ] Historical data storage
- [ ] Multiple ping protocols (TCP, UDP)
- [ ] Bulk target import/export

### 📖 Documentation
- [ ] API documentation
- [ ] Deployment guides
- [ ] Performance tuning guide
- [ ] Architecture documentation

### 🧪 Testing
- [ ] Unit tests
- [ ] Integration tests
- [ ] E2E tests
- [ ] Performance benchmarks

## Release Process

1. Update version in relevant files
2. Update CHANGELOG.md
3. Create a release PR
4. Tag the release
5. GitHub Actions will build and publish

## Getting Help

- 💬 [GitHub Discussions](https://github.com/nthewara/aznetmon/discussions) for questions
- 🐛 [GitHub Issues](https://github.com/nthewara/aznetmon/issues) for bugs
- 📧 Email maintainers for sensitive issues

## Recognition

Contributors will be:
- Listed in README.md
- Mentioned in release notes
- Added to CONTRIBUTORS.md (if they wish)

Thank you for making AzNetMon better! 🚀
