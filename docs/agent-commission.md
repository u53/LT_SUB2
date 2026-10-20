# 代理佣金模块

> 实施日期：2026-10-19
> 对应提交：`f39107e4b` 新增代理佣金模块；后续修复提交补充佣金开关与冻结期接线
> 前置设计：见 Plan 面板《代理佣金模块设计方案》

## 1. 业务模型

代理是一类普通用户，拥有两层价格：

```
平台 → 代理（成本价）：管理员按「代理 × 分组」配置
代理 → 下级（售价）：代理自己设置，不得低于成本

下级实付 = 目录价 × 售价倍率
代理成本 = 目录价 × 成本倍率
佣金     = 下级实付 − 代理成本
```

举例：某分组目录价为 1 元，管理员给代理 5 折，代理给下级 6 折。

| 项目 | 金额 |
|---|---|
| 下级实付 | 0.60 |
| 代理成本 | 0.50 |
| 佣金 | 0.10 |

**佣金基数用倍率前的目录价**（`TotalCost`），不是用户实付。这样佣金、用户实付、代理成本三者同源，可以精确对账：

```
佣金 + 代理成本 = 下级实付
```

## 2. 数据模型

迁移：`backend/migrations/292_agent_commission.sql`

| 表 | 用途 | 关键列 |
|---|---|---|
| `agent_profiles` | 代理档案，一用户一行 | `enabled`、`default_child_rate_multiplier`、`commission_frozen_hours` |
| `agent_group_costs` | 代理 × 分组成本折扣 | `(agent_user_id, group_id)` 主键、`cost_multiplier` |
| `agent_child_rates` | 代理给下级设置的售价 | `agent_user_id`、`child_user_id`、`group_id`（NULL 为全局默认）、`rate_multiplier` |
| `agent_commission_ledger` | 逐笔佣金流水 | `request_id` + `api_key_id` 唯一、`base_cost`、`child_rate`、`agent_cost_rate`、`child_paid`、`agent_cost`、`commission`、`status`、`frozen_until`、`reversal_of` |
| `agent_commission_accounts` | 佣金账户 | `available`、`frozen`、`withdrawn` |
| `agent_commission_withdrawals` | 提现记录 | `operation_id` 唯一幂等键 |

上下级关系**复用现有邀请体系**：`user_affiliates.inviter_id`，不另建关系表。一级代理的下级就是该用户的直接邀请人关系。

## 3. 计费接入

### 3.1 两条网关

Claude/Gemini 与 OpenAI 是两套并行的计费实现，必须同时改：

| 网关 | 文件 |
|---|---|
| Claude / Gemini | `backend/internal/service/gateway_usage_billing.go` |
| OpenAI | `backend/internal/service/openai_gateway_usage.go` |

两条路径都在算出 `cost` 之后调用 `resolveAgentCommission`，得到佣金快照，随后填入 `postUsageBillingParams`。

### 3.2 解析链

`AgentCommissionResolver`（`agent_commission_resolver.go`）按以下顺序解析：

1. 查 `user_affiliates.inviter_id` 得到直接上级；无上级 → 不计佣。
2. 自邀（`inviter_id == user_id`）→ 不计佣。
3. 代理档案不存在或 `enabled=false` → 不计佣。
4. 售价倍率：下级在该分组的专属售价 > 代理设置的默认售价 > 本次请求实际生效的倍率。
5. 成本倍率：管理员为该代理在该分组配置的值；**未配置 → 差价为 0，不产生佣金**。

第 5 点是刻意的保守选择：少发佣金可以事后补发，按猜测的成本多发则难以追回。

解析结果缓存 60 秒；写折扣时调用 `InvalidateChild` 主动失效，避免"后台已改价、用户仍按旧价扣费"。

### 3.3 写入时机

佣金在 `usageBillingRepository.Apply` 的**同一事务**内写入（`applyAgentCommissionEffect`）：

- 与余额/订阅/配额扣减同事务，扣费失败时佣金一起回滚。
- 幂等键复用 `(request_id, api_key_id)`；重放时流水不重复插入，账户不重复加钱。
- 零佣金（售价不高于成本、低于阈值）直接跳过，不产生流水。

### 3.4 佣金金额的精度

三个金额都量化到 8 位小数（与 `users.balance` 一致），并保证：

```
child_paid − agent_cost == commission
```

实现方式：先分别量化实付与成本，再用 decimal 域相减并量化佣金。如果直接对未量化的差值单独量化，会破坏这个恒等式。

## 4. 关键修复：折扣必须同步到计费表

**问题**：代理设置下级折扣时，最初只写入了 `agent_child_rates`。但计费实际读取的是 `user_group_rate_multipliers`。结果是代理以为改了价，用户仍按原价被扣费，佣金也按错误售价计算。

**修复**（`AgentService.syncChildBillingRate`）：

1. 先把代理侧记录写入 `agent_child_rates`（含归属与成本下限校验）。
2. 再读该下级现有的全部分组倍率，只替换目标分组，写回 `user_group_rate_multipliers`。

只改目标分组是有意的：`SyncUserGroupRates` 是全量替换语义，直接传单键 map 会清空该用户其他分组的专属倍率。

两步不是同一个事务。若第二步失败，代理侧保留记录但售价未生效，重试即可修复；反过来先写计费侧则可能出现"已按新价扣费但代理侧无记录"。

## 5. 权限模型

新增角色 `agent`（`domain.RoleAgent`）。

| 能力 | admin | agent | user |
|---|---|---|---|
| 管理端代理列表 / 成本折扣 / 提现登记 | 是 | 否 | 否 |
| 代理自助：概览 / 下级 / 折扣 / 流水 / 提现 | 是（可跨代理） | 仅自己 | 否 |
| 修改他人折扣 | 是 | 否（服务层拒绝） | 否 |

三层防线：

1. 路由层 `requireAgentRole`：非 agent（及 admin）返回 403。
2. 服务层归属校验：`GetInviterID(childID) == agentUserID`，否则 403。
3. 成本下限校验：售价低于代理在该分组的成本时拒绝，并返回明确的中文原因。

代理不能：修改分组、修改账号、把自己提权、操作非下级用户。

## 6. API

### 6.1 管理端

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/admin/agents` | 代理列表（分页、搜索、启用筛选） |
| GET | `/api/v1/admin/agents/:id` | 代理详情（含分组成本折扣） |
| PUT | `/api/v1/admin/agents/:id` | 启用/停用、默认下级折扣、冻结期、备注 |
| GET/PUT | `/api/v1/admin/agents/:id/group-costs` | 分组成本折扣（全量替换） |
| GET | `/api/v1/admin/agents/:id/children` | 下级列表 |
| GET | `/api/v1/admin/agents/commissions` | 佣金流水（按代理/下级/状态/时间筛选） |
| GET | `/api/v1/admin/agents/withdrawals` | 提现记录 |
| POST | `/api/v1/admin/agents/:id/withdraw` | 提现登记（需 `Idempotency-Key`） |

### 6.2 代理自助

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/user/agent/profile` | 佣金账户、邀请码、最低提现额 |
| GET | `/api/v1/user/agent/children` | 我的下级 |
| PUT | `/api/v1/user/agent/children/:id/rate` | 设置下级售价（`rate` 或 `rate_multiplier`） |
| GET | `/api/v1/user/agent/commissions` | 我的佣金流水 |
| POST | `/api/v1/user/agent/withdraw` | 发起提现 |
| GET | `/api/v1/user/agent/withdrawals` | 我的提现记录 |

## 7. 前端页面

| 路径 | 页面 | 说明 |
|---|---|---|
| `/admin/agents/list` | 代理列表 | 卡片式行、成本折扣抽屉、下级抽屉 |
| `/admin/agents/commissions` | 佣金流水 | 筛选与状态徽章 |
| `/admin/agents/withdrawals` | 提现记录 | 登记提现（前端生成幂等键） |
| `/agent` | 代理中心 | 概览、邀请、下级折扣、流水、提现 |

侧边栏按角色显示：代理账户在“我的账户”区标题、个人资料、代理中心入口处显示「代理」标签；普通用户看不到代理中心。

## 8. 配置项

| 设置键 | 默认 | 说明 |
|---|---|---|
| `agent_commission_enabled` | 关闭 | 总开关。关闭时不写任何佣金流水、不动佣金账户 |
| `agent_min_commission` | `0` | 最低计佣阈值，低于该值的差价丢弃 |
| `agent_commission_frozen_hours` | `0` | 佣金冻结期（小时），0 = 立即进入可提现 |
| `agent_min_withdraw` | `0` | 提现最低额 |

两项设置都带 10 秒进程内缓存，管理员修改后最多 10 秒生效，避免每个请求都查一次 `settings` 表。

分组侧开关：`groups.agent_distribution_enabled`（迁移 `293_group_agent_distribution.sql`，默认 true）。关闭后该分组不产生佣金，但仍正常扣费。

## 9. 提现流程

当前是**管理员线下打款后登记**，不是自动打款：

1. 代理在代理中心发起提现，或管理员直接登记。
2. 系统校验金额 > 0、不超过可提现余额、不低于最低提现额。
3. 写 `agent_commission_withdrawals` 并扣减 `agent_commission_accounts.available`，同时累加 `withdrawn`。
4. 管理端幂等键为 `Idempotency-Key` 请求头；同一键重放不会二次扣款。

尚无：提现申请审批流、对接支付渠道自动打款、提现手续费。

## 10. 已知限制与后续

| 限制 | 说明 |
|---|---|
| 仅一级代理 | 取直接上级，不递归；多级分润需要新增层级路径与逐级规则 |
| 批量生图不计佣 | 批量图片走 `CaptureBatchImageBalance`，不经过 `Apply`，当前不产生佣金 |
| 退款不冲正 | 表结构预留 `reversal_of` 与 `reversed` 状态，但尚未实现退款回收 |
| 不支持全局默认折扣 | 售价存储在「用户 × 分组」维度，没有"全组默认"；设置全局默认会返回明确错误 |
| 未配置成本折扣不计佣 | 见 3.2 第 5 点 |
| 佣金不进入用户余额 | 独立佣金账户，需提现或后续增加"转入余额"能力 |
| 交易步骤非原子 | 代理侧记录与计费侧倍率分两步写，第二步失败时重试修复（见第 4 节） |

## 11. 上线检查清单

1. 应用迁移 `292`、`293`（启动时自动执行）。
2. 在系统设置中开启 `agent_commission_enabled`（默认关闭，不开就不产生佣金流水）。
3. 在用户管理中把目标用户角色改为 `agent`。
4. 在代理管理的「配置」中启用代理，并按分组配置成本折扣。
5. 代理登录代理中心，邀请下级并设置售价。
6. 抽查：下级产生用量后，确认 `agent_commission_ledger` 有对应流水，且 `child_paid − agent_cost == commission`。
