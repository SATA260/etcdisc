# etcdisc

`etcdisc` 是一个基于 `etcd` 构建的控制面注册中心（Control Plane Registry）。

当前版本已经覆盖 phase-1 控制面主链路，并补齐了 phase-2 的第一版集群运行时能力，提供以下能力：

- 服务注册与发现
- 心跳保活与服务端主动探活
- namespace 隔离与访问模式控制
- 配置发布、查询、订阅
- 基础 A2A `AgentCard` 注册与 capability 发现
- HTTP 与 gRPC 双接入层
- Provider / Consumer / Config 三套 SDK
- 管理 API 与简单控制台
- 集群成员管理、assignment leader、service owner、heartbeat/probe runtime failover

项目实现严格参考 `tmp/docs/` 下的需求、架构、SDD 和 TDD 文档。

## 项目定位

当前 `etcdisc` 分两层演进：

- phase-1：控制面注册、发现、配置、A2A、管理面
- phase-2：集群运行时 ownership、heartbeat 超时推进、主动探活调度、故障接管

当前重点仍然是 **CP-only**，优先保证状态机、一致性、watch 和 failover 行为正确。

`etcdisc` 解决的核心问题包括：

- Provider 把实例注册到服务端
- Consumer 通过快照和 watch 获取服务列表
- Config SDK 获取有效配置并持续订阅变更
- Agent 通过 `AgentCard` 和 capability 被发现
- 管理员通过 Admin API 和控制台完成基本运维操作

## 当前范围

### 已实现

- 只做控制面（CP）
- 只做模式 A：客户端直连 registry server
- 支持多 namespace
- 支持 HTTP 和 gRPC
- 支持第一版集群运行时：
  - worker member lease
  - assignment leader 选举
  - `namespace + service` 粒度 service owner
  - owner epoch fencing
  - heartbeat timeout supervisor
  - probe scheduler + worker pool
  - reconciliation + rebuild
- 支持 4 种健康模式：
  - `heartbeat`
  - `http_probe`
  - `grpc_probe`
  - `tcp_probe`
- 支持服务注册、更新、注销
- 支持 discovery snapshot + watch
- 支持配置中心 scope 叠加与 watch
- 支持 A2A AgentCard 与 capability 精确匹配
- 支持管理 API 和服务端渲染控制台

### 当前不在范围内

- 本地 agent / sidecar 模式
- 复杂协议协商
- 工作流 / 编排能力
- 完整平台化前端 UI
- 业务侧统一鉴权
- 动态负载感知调度
- 每次节点新增都做全量 rebalance
- 按实例粒度 ownership

## 核心能力

### 1. 服务注册

- 基于 `namespace + service + instanceId` 管理实例
- `instanceId` 支持客户端可选传入，不传时服务端生成
- 支持显式更新实例
- 更新接口使用 `CAS`，统一依赖 `expectedRevision`
- 严格校验 `service`、`weight`、`metadata`、`healthCheckMode`

### 2. 健康管理

- `heartbeat` 模式：实例 key 绑定 etcd lease，服务端维护 `health -> unhealth -> delete`
- `probe` 模式：实例 key 不绑定 lease
- 支持 HTTP / gRPC / TCP 主动探活
- 状态机语义：`health -> unhealth -> delete`
- `delete` 直接物理删除 etcd 中的实例 key

### 2.1 集群运行时管理

- worker 通过 member lease 加入集群
- assignment leader 负责 service owner 分配与重分配
- owner 粒度固定为 `namespace + service`
- 任意节点都可接收 register / heartbeat / update
- runtime health 状态推进只由当前 owner 执行
- owner 驱动写入同时校验：
  - `ownerEpoch`
  - `instanceRevision`
- worker failover 后可从 etcd 事实状态 rebuild runtime task

### 3. 服务发现

- 支持实例快照查询
- `healthyOnly=true` 为默认行为
- 支持按 namespace、service、group、version、metadata 过滤
- HTTP 使用 SSE 推送 watch 事件
- gRPC 使用 Server Streaming 推送 watch 事件

### 4. 配置中心

- 支持 3 层配置作用域：
  - `global`
  - `namespace`
  - `service`
- 有效配置解析顺序：

  `global < namespace < service < client override`

- 支持 `valueType`：
  - `string`
  - `int`
  - `bool`
  - `duration`
  - `json`
- 支持配置 watch
- 删除低层配置后自动回退到上层配置

### 5. A2A 能力

- `AgentCard` 作为独立资源存储
- 基于 capability 建立索引
- capability 使用字符串精确匹配
- 支持可选 protocol 过滤
- 实际调用地址来自运行时实例，不存放在 `AgentCard` 内
- 支持 `authMode`：
  - `none`
  - `static_token`
  - `mtls`

### 6. 管理能力

- namespace 创建、查询、访问模式更新
- 配置项发布、查询、删除
- 服务与实例状态查看
- AgentCard / capability 查看
- 审计日志查看
- 系统状态与 metrics 查看
- 简单控制台页面

## 架构概览

高层处理链路如下：

1. `HTTP` / `gRPC` handler 负责请求解析与响应输出
2. `service` 层负责业务规则、校验、状态机和查询语义
3. `etcd adapter` 负责持久化、事务、lease 和 watch
4. `cluster runtime` 负责成员发现、ownership、heartbeat/probe 调度
5. SDK 侧采用 `snapshot -> watch -> local cache` 模型

设计原则：

- handler 不直接操作 etcd
- 关键元数据采用 `CAS`
- 高频运行时状态采用 `LWW`
- discovery 流和 config 流独立建模
- 本地调度队列只是执行缓存，etcd 中的事实状态才是权威来源

## 仓库结构

```text
etcdisc/
  api/proto/etcdisc/v1/        phase-1 gRPC 合同层
  cmd/etcdisc-server/          服务入口
  deployments/                 Dockerfile、compose、默认配置
  internal/api/                HTTP / gRPC 接入层
  internal/app/                bootstrap 与依赖装配
  internal/core/               model、service、port、keyspace、errors
  internal/infra/              etcd、logging、metrics、clock
  internal/runtime/            cluster ownership 与 runtime health scheduler
  pkg/                         对外 SDK 与公共 DTO
  test/e2e/                    HTTP / gRPC 端到端测试
  test/integration/            真实 etcd 集成测试
  test/testkit/                内存 store 与测试辅助组件
  tmp/docs/                    需求、架构、实施计划、TDD 文档
```

## 开发环境要求

默认要求所有开发、测试和运行操作都在 WSL 中完成。

推荐环境：

- WSL Ubuntu
- Go 1.25+
- Docker / Docker Compose
- 可用的本地 etcd（默认 `127.0.0.1:2379`）

## 快速开始

### 1. 启动完整本地环境

```bash
make up
```

默认会启动：

- `etcd`：`127.0.0.1:2379`
- HTTP 服务：`127.0.0.1:8080`
- gRPC 服务：`127.0.0.1:9090`

停止环境：

```bash
make down
```

### 2. 直接运行服务

如果你已经有可用的 etcd：

```bash
make run
```

### 3. 运行测试

普通测试：

```bash
make test
```

真实 etcd 集成测试：

```bash
make integration
```

集群运行时关键覆盖已经包含在 `go test ./...` 中，主要包括：

- assignment leader / member lease
- service owner assignment / epoch fencing
- heartbeat timeout supervisor
- probe scheduler / worker pool
- reconciliation / rebuild
- clustered runtime E2E

### 4. 统计覆盖率

```bash
go test -count=1 ./... -coverprofile=coverage.out
go tool cover -func=coverage.out
```

如果用于评估业务代码覆盖率，建议排除生成文件：

- `api/proto/etcdisc/v1/etcdisc.pb.go`
- `api/proto/etcdisc/v1/etcdisc_grpc.pb.go`
- `api/proto/etcdisc/v1/convert.go`

当前业务代码覆盖率已经达到 `80%+`。

## 配置说明

默认配置文件：

- `deployments/etcdisc.yaml`
- `deployments/etcdisc-cluster.yaml`

常用环境变量覆盖项：

- `ETCDISC_CONFIG_FILE`
- `ETCDISC_APP_NAME`
- `ETCDISC_APP_ENV`
- `ETCDISC_HTTP_HOST`
- `ETCDISC_HTTP_PORT`
- `ETCDISC_GRPC_HOST`
- `ETCDISC_GRPC_PORT`
- `ETCDISC_ETCD_ENDPOINTS`
- `ETCDISC_ETCD_DIAL_MS`
- `ETCDISC_CLUSTER_ENABLED`
- `ETCDISC_CLUSTER_NODE_ID`
- `ETCDISC_CLUSTER_ADVERTISE_HTTP_ADDR`
- `ETCDISC_CLUSTER_ADVERTISE_GRPC_ADDR`
- `ETCDISC_CLUSTER_MEMBER_TTL_SECONDS`
- `ETCDISC_CLUSTER_MEMBER_KEEPALIVE_SECONDS`
- `ETCDISC_CLUSTER_LEADER_TTL_SECONDS`
- `ETCDISC_CLUSTER_LEADER_KEEPALIVE_SECONDS`
- `ETCDISC_ADMIN_TOKEN`

本地默认管理员 Token：

```text
change-me-in-dev
```

## HTTP 接口

### 基础运行接口

- `GET /healthz`
- `GET /ready`
- `GET /metrics`

### Registry

- `POST /v1/registry/register`
- `POST /v1/registry/heartbeat`
- `POST /v1/registry/update`
- `POST /v1/registry/deregister`

### Discovery

- `GET /v1/discovery/instances`
- `GET /v1/discovery/watch`

### Config

- `GET /v1/config/effective`
- `GET /v1/config/watch`

### A2A

- `POST /v1/a2a/agentcards`
- `GET /v1/a2a/discovery`

### Admin API

管理员接口需要携带：

```text
Authorization: Bearer <admin-token>
```

接口列表：

- `GET|POST /admin/v1/namespaces`
- `PATCH /admin/v1/namespaces/{name}`
- `GET|POST|DELETE /admin/v1/config/items`
- `GET /admin/v1/services/instances`
- `GET /admin/v1/agentcards`
- `GET /admin/v1/audit`
- `GET /admin/v1/system`
- `GET /admin/v1/system/metrics`

### 控制台页面

- `/console/namespaces`
- `/console/services`
- `/console/policies`
- `/console/a2a`
- `/console/audit`
- `/console/system`

## gRPC 服务

phase-1 当前暴露的 gRPC 服务位于 `api/proto/etcdisc/v1/`：

- `RegistryService`
- `DiscoveryService`
- `ConfigService`
- `A2AService`

当前实现采用轻量级 JSON codec 方式承载 phase-1 gRPC 合同，而不是完整生成式 protobuf 消息流。

## 示例流程

### 1. 创建 namespace

```bash
curl -X POST http://127.0.0.1:8080/admin/v1/namespaces \
  -H 'Authorization: Bearer change-me-in-dev' \
  -H 'Content-Type: application/json' \
  -d '{"name":"prod-core"}'
```

### 2. 注册服务实例

```bash
curl -X POST http://127.0.0.1:8080/v1/registry/register \
  -H 'Content-Type: application/json' \
  -d '{
    "instance": {
      "namespace": "prod-core",
      "service": "payment-api",
      "instanceId": "node-1",
      "address": "127.0.0.1",
      "port": 8080,
      "healthCheckMode": "heartbeat"
    }
  }'
```

### 3. 查询发现结果

```bash
curl "http://127.0.0.1:8080/v1/discovery/instances?namespace=prod-core&service=payment-api"
```

### 4. 发布配置

```bash
curl -X POST http://127.0.0.1:8080/admin/v1/config/items \
  -H 'Authorization: Bearer change-me-in-dev' \
  -H 'Content-Type: application/json' \
  -d '{
    "item": {
      "scope": "service",
      "namespace": "prod-core",
      "service": "payment-api",
      "key": "timeout.request",
      "value": "1000",
      "valueType": "duration"
    }
  }'
```

### 5. 读取有效配置

```bash
curl "http://127.0.0.1:8080/v1/config/effective?namespace=prod-core&service=payment-api"
```

### 6. 上报 AgentCard

```bash
curl -X POST http://127.0.0.1:8080/v1/a2a/agentcards \
  -H 'Content-Type: application/json' \
  -d '{
    "card": {
      "namespace": "prod-core",
      "agentId": "agent-1",
      "service": "agent-api",
      "capabilities": ["tool.search"],
      "protocols": ["grpc"],
      "authMode": "static_token"
    }
  }'
```

### 7. 按 capability 发现 Agent

```bash
curl "http://127.0.0.1:8080/v1/a2a/discovery?namespace=prod-core&capability=tool.search&protocol=grpc"
```

## SDK 说明

### Provider SDK

位置：

- `pkg/provider/`

提供能力：

- 注册实例
- 发送 heartbeat
- 注销实例
- HTTP transport
- gRPC transport

### Consumer SDK

位置：

- `pkg/consumer/`

提供能力：

- snapshot + watch 本地缓存
- `round_robin`
- `random`
- `weighted_random`
- `consistent_hash`
- HTTP transport
- gRPC transport

### Config SDK

位置：

- `pkg/configsdk/`

提供能力：

- 有效配置快照
- watch 驱动缓存更新
- HTTP transport
- gRPC transport

## 测试说明

当前仓库包含：

- service 层单元测试
- handler 层单元测试
- SDK 单元测试
- 真实 etcd 集成测试
- HTTP / gRPC E2E 测试
- cluster runtime failover 测试

推荐测试顺序：

1. `make test`
2. 启动 etcd 或完整本地环境
3. `make integration`
4. 必要时执行覆盖率统计

## 开发约定

- handler 只负责协议层，不直接操作 etcd
- 关键元数据走 `CAS`
- 高频运行时状态走 `LWW`
- discovery 和 config 客户端统一采用：

  `snapshot -> watch -> local cache`

- namespace 是显式资源，不是字符串约定
