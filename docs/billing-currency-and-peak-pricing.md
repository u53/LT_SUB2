# 计费币种与峰谷定价改造

> 实施日期：2026-10-19
> 对应提交：`76c79de71` feat: 支持人民币记账与可配置峰谷定价
> 前置分析：见 [billing-cny-analysis.md](billing-cny-analysis.md)

## 1. 背景

改造前，系统以美元记账，国产模型的人民币官网价通过固定汇率折算后录入（代码注释里写的是 `÷7.14`）。这带来三个问题：

- **无法与官网对账**：录入值与官网价永远对不上。
- **隐含汇率不一致**：DeepSeek 官方美元价与人民币价隐含汇率约为 6.67（Flash）/ 6.82（Pro），统一按 7.14 折算会系统性低估国产模型成本。
- **峰谷不可配**：DeepSeek 的峰谷价写死在 Go 常量里，运营者无法查看或调整，且缺少法定节假日判定。

本次改造的目标是：**国产模型直接录人民币官网价，海外模型保留美元官网价，峰谷成为可配置数据**。

## 2. 币种模型

### 2.1 两个概念

| 概念 | 含义 | 存储位置 |
|---|---|---|
| 记账币种 | 余额、用量成本、配额、佣金的单位 | `billing.accounting_currency` |
| 价格录入币种 | 某条价格按什么币种录入 | 价格条目的 `currency` 字段 |

价格条目在计费前按汇率折成记账币种；折扣倍率与币种无关，因此在折算之后应用。

### 2.2 配置项

```yaml
billing:
  accounting_currency: CNY      # 记账币种；USD 或留空保持历史行为
  usd_to_accounting_rate: 7.1   # 1 USD = 7.1 CNY
  # 额外币种折算；USD 写在这里可覆盖上面的汇率
  currency_rates:
    USD: 7.2
```

| 配置键 | 默认值 | 说明 |
|---|---|---|
| `billing.accounting_currency` | `USD` | 记账币种，ISO 4217。留空或 `USD` 保持历史行为 |
| `billing.usd_to_accounting_rate` | `0` | 1 USD 折合记账币种。非 USD 记账时必须配置 |
| `billing.currency_rates` | `{}` | 额外币种折算表；`USD` 写在这里可覆盖上一项 |

非 USD 记账若缺少 USD 汇率，**启动即失败**，避免运行期静默按 1:1 少计费。

### 2.3 数据列

| 对象 | 列/字段 | 迁移 |
|---|---|---|
| 渠道价格 | `channel_model_pricing.currency` | `291_channel_pricing_currency.sql` |
| 分组价格 | `groups.model_pricing` JSON 内的 `currency` | 无需迁移（JSONB） |
| 代码结构 | `ModelPricing.Currency`、`ChannelModelPricing.Currency` | — |

`currency` 为空表示"与记账币种相同"。这一约定保证存量配置升级后语义不变。

### 2.4 折算规则

实现位于 `backend/internal/service/pricing_currency.go`。

1. 条目币种为空，或与记账币种相同 → 原样返回，不引入浮点误差。
2. 条目币种不同 → 乘以汇率，并清空 `Currency`，防止二次折算。
3. 缺少汇率 → **保留原价并告警一次**（每模型每进程一条），不静默按 1:1 处理。
4. 折算只作用于价格，不作用于已发生的金额。余额、配额、用量成本始终是记账币种。

## 3. DeepSeek 人民币官网价

DeepSeek 官方同时提供人民币与美元两张价目表，两者不是同一汇率换算的结果，因此按记账币种选择对应表，而不是维护一套再折算。

| 模型 | 缓存命中（低谷） | 缓存未命中（低谷） | 输出（低谷） |
|---|---|---|---|
| deepseek-flash | ¥0.02 / MTok | ¥1 / MTok | ¥4 / MTok |
| deepseek-v4-pro | ¥0.15 / MTok | ¥4.5 / MTok | ¥13.5 / MTok |

美元表对应为 `$0.003 / $0.15 / $0.60` 与 `$0.022 / $0.66 / $1.98`。

**峰谷口径**：高峰价 = 2 × 低谷价。高峰时段为北京时间周一至周五（不含中国法定节假日）09:00–12:00 与 14:00–18:00；其余时段（含周末与法定节假日全天）为低谷。

**Pro 的计费口径**：官方公告（2026-09-10）说明自 2026-09-14 04:00 UTC 起，`deepseek-v4-pro` 请求全部路由到 V4.1-Flash 并按 Flash 价格计费，直到 V4.1-Pro 上线。代码保留切换时点判断，历史请求可按当时口径复算。

## 4. 峰谷时段引擎

原来的三套机制（DeepSeek 硬编码峰谷、渠道分时倍率、分组高峰倍率）统一为**时段价档**，实现位于 `backend/internal/service/custom_channel_time_pricing.go`。

### 4.1 结构

```go
type ChannelTimePricing struct {
    Timezone     string
    WeekdaysOnly bool
    Holidays     []string                 // YYYY-MM-DD，命中当日按基础价
    Periods      []ChannelTimePricingPeriod
}

type ChannelTimePricingPeriod struct {
    StartTime       string
    EndTime         string
    Multiplier      float64  // 历史语义：倍率
    InputPrice      *float64 // 绝对价：填写后直接替换单价
    OutputPrice     *float64
    CacheWritePrice *float64
    CacheReadPrice  *float64
}
```

**绝对价优先于倍率**：上游给出两个独立价格而不是简单倍数时，直接替换对应分项的单价。

### 4.2 判定顺序

1. 时区非法或配置脏 → 安全降级为不改价。
2. `weekdays_only` 且为周末 → 基础价。
3. 命中 `holidays` → 基础价。
4. 命中时段 → 绝对价替换，或用倍率缩放；未命中 → 基础价。

### 4.3 解除了三个原有约束

| 原约束 | 现状 |
|---|---|
| 分组逐模型定价禁止配置分时（返回 `GROUP_MODEL_TIME_PRICING_UNSUPPORTED`） | 已解除。分组价可以同时带峰谷时段 |
| DeepSeek 峰谷只对默认价卡生效 | 时段表跟随价格条目；渠道/分组自定义价也可配峰谷 |
| 展示路径会套用当前时刻的分时倍率 | 展示路径设置 `DisableTimePricing`，阶梯表始终表示标准时段价 |

同时修复了一个既有缺陷：渠道分时在 `PricingAt` 为零值时静默失效（`MultiplierAt` 直接返回 1.0）；现在使用已回退的时刻。

## 5. 法定节假日数据

节假日日期无法从公历推算：春节、清明、端午、中秋依赖农历与节气，且每年由国务院办公厅发布《关于次年部分节假日安排的通知》确定具体放假与调休日期。

因此节假日改为**数据驱动**：

- 数据文件：`backend/internal/calendar/cn_holidays.json`（随二进制嵌入）
- 加载代码：`backend/internal/calendar/holiday.go`
- 数据来源：每年的通知原文（文件中记录 `source` 字段）
- 维护方式：国务院发布次年安排后追加对应年份，**不需要改计费代码**

**关于调休**：只收录"放假"日期。调休上班的周末仍按周末处理，因为上游口径是"周一至周五（不含法定节假日）"，周末即使调休上班也不属于高峰。

数据缺失时按"仅周末低谷"处理，这是保守方向（不会多收费）。

## 6. 兼容性与行为变化

| 场景 | 行为 |
|---|---|
| 存量价格条目（`currency` 为空） | 语义不变 |
| 存量分组（`agent_distribution_enabled` 默认 true） | 语义不变 |
| 美元记账部署 | 行为与改造前一致 |
| 渠道分时的零值时点 | 由"静默失效"变为"正常生效" |
| 分组定价 + 分时 | 由"拒绝保存"变为"允许" |

## 7. 部署步骤

1. 应用迁移 `291_channel_pricing_currency.sql`（自动执行）。
2. 若切换记账币种为 CNY：
   - 配置 `billing.accounting_currency: CNY`
   - 配置 `billing.usd_to_accounting_rate`（如 `7.1`）
   - **不要**同时保留 `BALANCE_RECHARGE_MULTIPLIER` 的旧折算，否则会二次折算
3. 在管理端为国产模型的价格条目填写 `currency: CNY`，并录入官网价。
4. 重启服务，确认日志中没有 `Missing FX rate` 告警。

**注意**：本次改造**没有换算历史数据**。`usage_logs` 历史行与用户余额仍按原币种数值保存；要真正切换记账币种，需要单独的数据迁移（余额、配额、预聚合表），风险较高，应单独规划。

## 8. 已知限制

- 汇率是静态配置项，不会自动更新。
- 节假日数据只覆盖文件中已收录的年份；未收录年份按"仅周末"处理。
- 内置节假日表按中国法定节假日维护；其他地区的峰谷需要自行配置 `holidays`。
- 历史金额未换算（见第 7 节）。
