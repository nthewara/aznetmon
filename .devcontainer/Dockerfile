FROM golang:1.24.2

# Install basic development tools
RUN apt update && apt install -y \
    git \
    make \
    wget \
    curl \
    iputils-ping \
    sudo \
    && rm -rf /var/lib/apt/lists/*

# Add non-root user with sudo access
ARG USERNAME=vscode
ARG USER_UID=1000
ARG USER_GID=$USER_UID

RUN groupadd --gid $USER_GID $USERNAME \
    && useradd --uid $USER_UID --gid $USER_GID -m $USERNAME \
    && echo $USERNAME ALL=\(root\) NOPASSWD:ALL > /etc/sudoers.d/$USERNAME \
    && chmod 0440 /etc/sudoers.d/$USERNAME

# Create workspace directory and set permissions
RUN mkdir -p /workspace && chown $USER_UID:$USER_GID /workspace

# Install Go development tools
RUN go install github.com/air-verse/air@latest && \
    go install github.com/cosmos/gosec/v2/cmd/gosec@latest && \
    go install golang.org/x/tools/gopls@latest && \
    go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest && \
    go install github.com/go-delve/delve/cmd/dlv@latest

# Set the working directory
WORKDIR /workspace

# Add Go bin directory to PATH
ENV PATH=$PATH:/go/bin

# Switch to non-root user
USER $USERNAME