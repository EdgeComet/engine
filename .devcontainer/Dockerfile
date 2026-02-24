FROM golang:1.24.2-bookworm

# Define non-root user credentials for a secure development environment.
ARG USERNAME=dev
ARG USER_UID=1000
ARG USER_GID=$USER_UID

# Create non-root user and configure passwordless sudo.
RUN groupadd --gid $USER_GID $USERNAME \
    && useradd --uid $USER_UID --gid $USER_GID -m -s /bin/bash $USERNAME \
    && apt-get update \
    && apt-get install -y sudo \
    && echo "$USERNAME ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/$USERNAME \
    && chmod 0440 /etc/sudoers.d/$USERNAME

# Download and install git-bug binary x64 linux to system path.
RUN curl -L https://github.com/git-bug/git-bug/releases/download/v0.10.1/git-bug_linux_amd64 \
    -o /usr/local/bin/git-bug \
    && chmod +x /usr/local/bin/git-bug

# Install fixed Air version for reproducible builds.
RUN go install github.com/air-verse/air@v1.61.7

# Install tools for go 1.24
RUN go install -v golang.org/x/tools/gopls@v0.18.1

RUN go install -v github.com/go-delve/delve/cmd/dlv@v1.24.0 && \
    go install -v honnef.co/go/tools/cmd/staticcheck@v0.6.1

# Set working directory.
WORKDIR /workspace

# Extend PATH to include Go binaries.
ENV PATH="/go/bin:${PATH}"
ENV GOMODCACHE=/home/${USERNAME}/go/pkg/mod

# Expose application ports.
EXPOSE 3000 8000

# Switch to non-root user.
USER $USERNAME