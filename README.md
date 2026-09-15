# NetDrive Sweeper · 网盘垃圾文件清理器

面向 **115 / 123 网盘**（经 CloudDrive2 挂载/托管）的自动垃圾文件清理器。
单二进制 / Docker 部署，直连 **CloudDrive2 gRPC API**，**不上挂载点、不做高频轮询**，从设计上规避网盘风控。

> 典型场景：BT 离线下载的影视资源目录里，混着一堆 `.txt` 广告、`.url`/`.lnk` 快捷方式、`.html` 推广页，以及体积很小的引流视频（< 20 MB）。本工具在事件驱动下把它们就地清掉。

---

## 1. 它解决什么问题

| 旧做法（本地 FUSE 挂载 + 遍历） | 本工具（gRPC 直连） |
| --- | --- |
| `os.ReadDir` / `os.Lstat` 每次都是网络回源，容易触发风控 | 一次 `GetSubFiles` 等价一次服务端 readdir，调用次数完全可控 |
| 需要把云盘挂载进容器，权限/属主复杂 | 只连 CD2 的 gRPC 端口，无需挂载云盘目录 |
| 定时轮询全树 = 高频回源 | 令牌桶限速 + **PushMessage 事件驱动** |

---

## 2. 核心设计原则（风控安全）

1. **直连 gRPC，不经 FUSE**：所有目录读取走 CloudDrive2 的 `GetSubFiles`，删除走 `DeleteFile` / `DeleteFilePermanently`。
2. **全局令牌桶限速**：默认 `5 ops/s`，对齐 115 官方 `maxQueriesPerSecondLimit`。每一个 gRPC 调用前先取令牌。
3. **事件驱动而非轮询**：常驻订阅 `PushMessage` 流，收到 `FILE_SYSTEM_CHANGE` 事件后**防抖合并**再触发扫描。**禁止**任何「每 N 秒扫全树」的循环。
   - 订阅由 `pushSupervisor` 常驻管理：进程启动 + **每次保存配置** + 20s 兜底巡检时按当前配置
     热启动 / 热停止 / 热重启订阅。该巡检**只读内存中的配置、不发任何网络请求**，不是扫全树。
   - 订阅的真实状态（`运行中`/`权限不足`/`未启用`/`连接失败`）由 `/api/state` 与 `/api/push`
     暴露给页面，因此「事件驱动到底有没有在跑」不再需要靠猜或翻日志。
4. **删除默认进网盘回收站**：默认调用 `DeleteFile`（进回收站，可恢复）；永久删除需显式开启 `delete_permanently`。
5. **多重保险丝**：
   - **删除总开关** `allow_delete` 默认关闭，不开则不执行任何删除；
   - **单文件冷却**：`file_cooldown_hours` 大于 0 时，新写入未满该小时数的文件跳过（默认 `0` = 立即清理，不冷却）；
   - **未完成下载保护**：目录含 `.part`/`.!qB`/`.crdownload` 等未完成后缀时**整目录跳过**；
   - **单轮上限** `max_files_per_run`，达上限即中止本轮；
   - **Token 权限自检**：缺 `allow_delete` 等权限时直接拒绝执行并报错。
6. **错误不吞**：CD2 不可达 / Token 失效 / 权限不足均给出明确中文提示。

---

## 3. 工作原理

```
事件驱动（推荐）:
  CD2 PushMessage 流 ──► 收到 FILE_SYSTEM_CHANGE(4)
                          └─► 防抖 N 秒（合并突发事件）
                                └─► 扫描配置的多目录（DFS，每次 GetSubFiles 前取令牌）
                                      └─► 命中规则 + 通过全部保险丝 ──► 删除（回收站/永久）

手动触发:
  Web 页面「预览扫描」/「立即清理」 ──► 走同一套扫描与校验逻辑
```

命中规则：
- **后缀命中**：`ad_exts`（默认 `.txt,.html,.url,.lnk`）→ 直接命中；
- **小体积视频**：`video_exts`（默认 `.mp4,.mkv,.ts`）且体积 ≤ `size_limit_mb`（默认 20 MB）→ 命中；
- **排除目录**：`exclude_dirs`（默认 `重要,备份`）名称包含关键词即整目录跳过。

---

## 4. 前置条件

1. 已安装并运行 **CloudDrive2**，且已登录网盘账号、挂载了目标云盘。
2. CD2 已开启 **gRPC 服务**，监听 `127.0.0.1:19798`（默认，可在 CD2 设置中修改；启用路径：CD2 设置 → 启用 API / gRPC）。
3. 在 CD2 中创建一个 **API Token**，并授予权限：
   - `allow_list`（读目录）— **必需**
   - `allow_delete`（删除到回收站）— 需要清理时必需
   - `allow_delete_permanently`（永久删除）— 仅在开启永久删除时需要
   - `allow_push_message`（推送订阅）— 需要事件驱动实时清理时必需
4. 运行环境能访问 CD2 的 gRPC 端口。注意：随附的 `docker-compose.yml` 默认使用**桥接网络**，容器内的 `127.0.0.1` 指向容器自身，因此 CD2 地址需填**宿主机内网 IP**（如 `192.168.1.10:19798`）或 `host.docker.internal:19798`；若 CD2 与容器同机、想直接用 `127.0.0.1:19798`，可改用 `network_mode: host`。

---

## 5. 快速开始

### 5.1 Docker Compose（推荐）

```yaml
services:
  netdrive-sweeper:
    build: .
    image: liubangjian/netdrive-sweeper:latest
    container_name: netdrive-sweeper
    # 桥接网络：容器内的 127.0.0.1 指向容器自身，无法访问宿主机的 CD2。
    # 因此需在 Web 页面把 CD2 地址填成宿主机内网 IP（如 192.168.1.10:19798），
    # 或使用 host.docker.internal:19798（下方 extra_hosts 已做 host-gateway 映射）。
    ports:
      - "5055:5000"
    extra_hosts:
      - "host.docker.internal:host-gateway"
    volumes:
      - ./data:/app/data
    environment:
      - LISTEN=:5000
      - CONFIG_PATH=/app/data/config.json
      - RECORDS_PATH=/app/data/records.jsonl
      - LOG_PATH=/app/data/clean.log
    restart: unless-stopped
```

```bash
docker compose up -d
```

> 默认即桥接（bridge）模式，因此**必须**在 Web 页面把 CD2 地址填成宿主机内网 IP（如 `192.168.1.10:19798`）或 `host.docker.internal:19798`（上方 `extra_hosts` 已做 `host-gateway` 映射）。端口映射为**宿主机 5055 → 容器 5000**，可按需改回 `5000:5000`。若你希望容器直接用 `127.0.0.1:19798` 访问同机的 CD2，可改回 `network_mode: host`（此时需移除 `ports`，Web 端口即宿主机 5000）。

### 5.2 docker run

```bash
docker run -d \
  --name netdrive-sweeper \
  --network host \
  -v $(pwd)/data:/app/data \
  -e LISTEN=:5000 \
  liubangjian/netdrive-sweeper:latest
```

### 5.3 本地构建（可选）

```bash
go build -o netdrive-sweeper .
./netdrive-sweeper
```

启动后浏览器打开 `http://<主机IP>:5055`。

---

## 6. 首次配置（Web 页面）

页面只有两个页签：**① 连接 · 目录 · 规则**（配置）与 **② 扫描清理 · 记录**（执行与查看）。
**所有配置项保存后立即生效——包括事件驱动订阅，无需重启容器。**

1. **填写 CD2 地址与 Token**：Docker 桥接部署下填**宿主机内网 IP**（如 `192.168.1.10:19798`）或 `host.docker.internal:19798`（容器内的 `127.0.0.1` 指向容器自身，连不到宿主机 CD2）；仅 host 网络模式或程序直接跑在宿主机上时才用 `127.0.0.1:19798`。然后粘贴 CD2 里创建的 API Token。
2. 点「**测试连接**」：成功会显示 Token 根目录与已授予的权限（页顶徽章）。
3. **添加要清理的目录**（支持多目录）：点「＋ 添加目录」在弹框里逐层浏览选择。
4. **设置规则**：垃圾后缀、视频后缀、体积阈值、排除目录、最大递归深度（在「高级」里）。
5. **选择删除方式**：默认「进网盘回收站」；如需永久删除，勾选「永久删除」。
6. **打开删除总开关**「允许自动清理」——这是所有删除动作的前提。
   未开启时「扫描清理」只做扫描预览，不会删除任何文件。
7. **点「保存配置」**：配置落盘，事件驱动订阅按新配置自动重启（页顶「事件驱动」状态会变为`运行中`）。
8. 切到 **② 扫描清理 · 记录**，点「**扫描清理**」：开启删除总开关时会先弹确认框，
   确认后**同一轮内完成扫描与删除**；结果、清理记录与运行日志都在本页下方，并带执行时间。

> ⚠️ 建议先在**少量测试目录**上验证规则，确认无误后再放开到全盘。
> 💡 页首「事件驱动」状态是权威状态源：`运行中`=订阅正常，`权限不足`=Token 缺
> `allow_push_message`（去 CD2 补上后回页面保存即可，无需重启），`连接失败`=CD2 不可达。

---

## 7. 配置项说明

配置持久化在 `data/config.json`（容器内 `/app/data/config.json`），删除/重建容器不丢失。

| 键 | 默认值 | 说明 |
| --- | --- | --- |
| `address` | `127.0.0.1:19798` | CD2 gRPC 地址；Docker 桥接部署需改为宿主机内网 IP 或 host.docker.internal:19798 |
| `token` | 空 | CD2 API Token |
| `tasks` | `[]` | 要清理的目录列表；**空 = 不扫描任何目录**（不会隐式扫全盘） |
| `ad_exts` | `.txt,.html,.url,.lnk` | 垃圾后缀（直接命中） |
| `video_exts` | `.mp4,.mkv,.ts` | 视频后缀（按体积判定） |
| `size_limit_mb` | `20` | 小体积视频阈值（MB） |
| `exclude_dirs` | `重要,备份` | 排除目录关键词 |
| `max_depth` | `0` | 最大递归深度，`0` = 不限 |
| `offline_only` | `true` | 仅扫描离线任务已完成的目录 |
| `delete_permanently` | `false` | `false`=回收站，`true`=永久删除 |
| `allow_delete` | `false` | **删除总开关**，关闭则只扫描不删除 |
| `ops_per_sec` | `5.0` | 令牌桶速率（对齐 115 官方上限） |
| `burst` | `10` | 令牌桶突发容量 |
| `max_files_per_run` | `2000` | 单轮删除文件数上限（保险丝） |
| `max_total_bytes` | `10 GiB` | 单轮删除总字节上限 |
| `file_cooldown_hours` | `0` | 新文件冷却小时数，`0` = 立即清理（不冷却），`>0` 时写入未满该小时的文件会被跳过 |
| `incomplete_suffixes` | `.part,.download,.!qB,.bc!,.aria2,.crdownload,.td,.tmp,.!ut` | 含这些后缀的目录会被整目录跳过；留空将自动回填默认值，该保护不建议关闭 |
| `force_refresh` | `false` | 强制刷新 CD2 缓存（每次 `GetSubFiles` 绕过缓存回源网盘）；非必要不建议开启，会放大网盘请求量 |
| `enable_push` | `true` | 启用 PushMessage 事件驱动；**保存配置即热生效，无需重启容器** |
| `push_debounce_seconds` | `5` | 事件防抖秒数；保存配置即热生效 |
| `config_version` | `1` | 内部字段：配置迁移版本。旧配置（无此字段）启动时执行一次性迁移，如把旧默认 `file_cooldown_hours=6` 改为 `0` |

环境变量：`LISTEN`、`CONFIG_PATH`、`RECORDS_PATH`、`LOG_PATH`。

---

## 8. 运行记录

- **清理记录**：页面「清理记录」面板可查看每次删除的时间、路径、体积、模式（回收站/永久）、结果。落盘于 `data/records.jsonl`。
- **运行日志**：落盘于 `data/clean.log`，页面可查看、可一键清空。

---

## 9. 常见问题

| 现象 | 原因 / 处理 |
| --- | --- |
| 「CD2 不可达」 | CD2 未启动 / gRPC 未开启 / 端口不是 19798；**桥接模式下地址填了 `127.0.0.1` 也会不可达**，需改填宿主机内网 IP 或 `host.docker.internal:19798` |
| 「Token 无效或过期」 | Token 复制不完整，或已在 CD2 侧失效 |
| 「Token 权限不足」 | 缺 `allow_list` / `allow_delete` / `allow_push_message`，回到 CD2 重新勾选 |
| 勾选了清理但没删掉 | 删除总开关 `allow_delete` 未开（此时「扫描清理」只预览不删）；或 `file_cooldown_hours` 大于 0 且文件仍在冷却期（含刚完成的离线下载）；或目录含未完成后缀/离线任务未完成 |
| 事件驱动不生效 | 看页首「事件驱动」状态：`权限不足`=Token 缺 `allow_push_message`（补上后保存配置即生效，**无需重启容器**）；`未启用`=`enable_push` 关闭或地址/Token 为空；`连接失败`=CD2 不可达 |
| 加了离线任务却没有任何动作 | ① 页首「事件驱动」是否为`运行中`；② 「允许自动清理」是否已勾选；③ `file_cooldown_hours` 是否为 `0`。日志中会打印收到的 `messageType`，可据此确认 CD2 实际发出的推送类型 |
| 日志比之前少 | 已不再对「地址/Token 未配置」每 10s 刷一条重连失败日志（改为单条状态提示）；后台动作会打「事件驱动实时清理已启动」「收到文件系统变更事件」等日志 |
| 日志里大量「跳过未完成离线目录」 | 该目录离线任务未完成，属正常保护 |

---

## 10. 代码结构

```
.
├── main.go          # HTTP 服务、配置读写、扫描入口、PushMessage 消费装配
├── cd2client.go     # CD2 gRPC 客户端（dynamicpb + protocompile 动态解析 cd2.proto）
├── config.go        # 配置结构、默认值、归一化、持久化
├── limiter.go       # 令牌桶限速
├── sweeper.go       # 扫描 + 规则判定 + 保险丝 + 删除 + 审计记录
├── push.go          # PushConsumer：PushMessage 订阅 / 防抖 / 断线重连
├── web.go           # 内嵌 Web 界面（embed）
├── cd2.proto        # CD2 gRPC 精简 proto（运行时解析，无需 protoc）
├── main_test.go     # 单元测试
├── Dockerfile
├── docker-compose.yml
└── .github/workflows/docker-build.yml   # CI：构建并推送镜像到 Docker Hub
```

---

## 11. 免责声明

本工具会**删除网盘文件**。默认删除进回收站，但仍建议：
- 首次使用先在测试目录验证；
- 保持 `allow_delete` 关闭、用「预览扫描」确认规则；
- 重要目录务必加入 `exclude_dirs`。

因使用本工具造成的任何数据丢失，使用者自行承担。
