# 📚 Memory Index - Lambda Event Forwarder Project

## Project Overview
**Purpose**: AWS Lambda function that forwards events from ALB/SNS to multiple destinations
**Scale**: 200K messages/hour (55/sec)
**Language**: Go 1.21+
**Critical Requirements**: Always return 200 to ALB, no auth handling

## Memory Categories

### 1️⃣ **CORE_ARCHITECTURE** 
**File**: `memory_core_architecture.md`
- Handler signature (returns `any` not `error`)
- ALB always returns 200 pattern
- Event detection and routing logic
- Async processing architecture
- Why these patterns prevent retry storms

### 2️⃣ **IMPLEMENTATION_CODE**
**File**: `memory_implementation.md`
- Complete working code structure
- main.go implementation
- forwarder_handler.go details
- SNS and webhook forwarder implementations
- go.mod dependencies

### 3️⃣ **TESTING_VALIDATION**
**File**: `memory_testing.md`
- Unit test implementations
- Critical test cases (ALB 200, health checks, etc.)
- Benchmarking approach
- Integration testing patterns
- Test execution commands

### 4️⃣ **CONFIGURATION_DEPLOYMENT**
**File**: `memory_config_deploy.md`
- Environment variables required
- Build and deployment process
- AWS Lambda configuration
- GitHub Actions CI/CD setup
- Makefile commands

### 5️⃣ **REQUIREMENTS_CONSTRAINTS**
**File**: `memory_requirements.md`
- Original Go service architecture reference
- Performance requirements (200K msg/hour)
- No authentication handling requirement
- Health check filtering needs
- Event structure preservation

### 6️⃣ **TROUBLESHOOTING_PATTERNS**
**File**: `memory_troubleshooting.md`
- Common issues and solutions
- Debugging approaches
- Performance optimization insights
- Monitoring and alerting setup

## Quick Reference Commands

```bash
# When starting new chat about:
# - Architecture decisions → Look at `.claude/context/memory-core-architecture.md`
# - Code implementation → Look at `.claude/context/memory-implementation.md`  
# - Testing issues → Look at `.claude/context/memory-testing.md`
# - Deployment → Look at `.claude/context/memory-config-deploy.md`
# - Original requirements → Look at `.claude/context/memory-requirements.md`
# - Debugging → Look at `.claude/context/memory-troubleshooting.md`
```

## Context Restoration Example

```
"I need help with my Lambda forwarder project. Here's my memory index: [paste this index]"
