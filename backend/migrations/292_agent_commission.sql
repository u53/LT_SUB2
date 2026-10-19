-- 代理佣金模块的数据层：代理档案、代理×分组成本折扣、代理→下级售价覆盖、
-- 逐笔佣金流水与佣金账户。
--
-- 首期只做一级代理：下级只有一个直接邀请人（user_affiliates.inviter_id），
-- 佣金只归该邀请人，因此这里不建任何多级分佣的层级/递归表。
--
-- 全部语句幂等（IF NOT EXISTS），可在单个事务内重复执行。

-- 1) 代理档案：只有被管理员开通代理身份的用户才有这一行。
--    用 user_id 直接做 PK，意味着「一个用户最多一条代理档案」，也让
--    外键 ON DELETE CASCADE 顺带清理代理关系。
CREATE TABLE IF NOT EXISTS agent_profiles (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    default_child_rate_multiplier DECIMAL(10,4) NULL,
    commission_frozen_hours INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT agent_profiles_commission_frozen_hours_check CHECK (commission_frozen_hours >= 0)
);

COMMENT ON TABLE agent_profiles IS '代理档案：管理员开通代理身份的用户及其默认策略';
COMMENT ON COLUMN agent_profiles.user_id IS '代理的用户ID，同时是主键（一个用户最多一条代理档案）';
COMMENT ON COLUMN agent_profiles.enabled IS '代理是否启用；关闭后不再产生新佣金，历史流水与余额保留';
COMMENT ON COLUMN agent_profiles.default_child_rate_multiplier IS '代理给新下级的默认售价倍率（相对分组基础价的倍数）；NULL 表示未设置、由下级按分组默认价计费';
COMMENT ON COLUMN agent_profiles.commission_frozen_hours IS '佣金冻结时长（小时）。0 表示佣金立即转为可提现，>0 表示每笔佣金先进入冻结余额，到期后解冻';
COMMENT ON COLUMN agent_profiles.note IS '管理员备注（纯文本，不参与计费）';

-- 2) 代理×分组成本折扣：管理员给代理设定的成本价倍率。
--    代理视角的「成本」= 分组基础价 × cost_multiplier，下级售价与成本之间的
--    差额就是佣金。没有行的分组按 1.0（无折扣）处理。
CREATE TABLE IF NOT EXISTS agent_group_costs (
    agent_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    cost_multiplier DECIMAL(10,4) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (agent_user_id, group_id),
    CONSTRAINT agent_group_costs_cost_multiplier_check CHECK (cost_multiplier > 0)
);

CREATE INDEX IF NOT EXISTS idx_agent_group_costs_group
    ON agent_group_costs (group_id);

COMMENT ON TABLE agent_group_costs IS '代理×分组成本折扣：代理在该分组拿到的成本价倍率';
COMMENT ON COLUMN agent_group_costs.agent_user_id IS '代理的用户ID';
COMMENT ON COLUMN agent_group_costs.group_id IS '分组ID；分组删除时该折扣随之失效';
COMMENT ON COLUMN agent_group_costs.cost_multiplier IS '成本倍率（相对分组基础价的倍数，>0）：代理结算成本 = 基础成本 × 该值；无行时为 1.0';

-- 3) 代理→下级的售价覆盖。
--    group_id 为 NULL 表示该下级的全局默认售价倍率；非 NULL 表示只在指定分组生效。
--    取值优先级：精确分组 > group_id 为 NULL 的全局默认 > 分组的公开售价。
CREATE TABLE IF NOT EXISTS agent_child_rates (
    id BIGSERIAL PRIMARY KEY,
    agent_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    child_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id BIGINT NULL REFERENCES groups(id) ON DELETE CASCADE,
    rate_multiplier DECIMAL(10,4) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT agent_child_rates_rate_multiplier_check CHECK (rate_multiplier > 0)
);

-- 唯一范围必须忽略 NULL 语义差异：group_id 为 NULL 时 COALESCE 成 0，
-- 这样「全局默认」（group_id IS NULL）也只有一行——否则 NULL 之间互不相等，
-- 同一代理可以给同一下级插入多条全局默认，取值就变得不确定。
CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_child_rates_scope
    ON agent_child_rates (agent_user_id, child_user_id, COALESCE(group_id, 0));

CREATE INDEX IF NOT EXISTS idx_agent_child_rates_child
    ON agent_child_rates (child_user_id);

COMMENT ON TABLE agent_child_rates IS '代理→下级售价覆盖：代理给下级设置的分组售价倍率';
COMMENT ON COLUMN agent_child_rates.agent_user_id IS '代理（邀请人）的用户ID';
COMMENT ON COLUMN agent_child_rates.child_user_id IS '下级（被邀请人）的用户ID';
COMMENT ON COLUMN agent_child_rates.group_id IS '分组ID；NULL 表示该下级的全局默认售价倍率';
COMMENT ON COLUMN agent_child_rates.rate_multiplier IS '售价倍率（相对分组基础价的倍数，>0）；下级按 基础成本 × 该值 付费';

-- 4) 逐笔佣金流水：一次请求用量对应最多一行，靠 (request_id, api_key_id) 幂等。
--    金额列用 DECIMAL(20,10) 承载倍率相乘后的中间精度，
--    账户表再收敛到 DECIMAL(20,8)（与 users.balance / api_keys.quota_used 一致）。
CREATE TABLE IF NOT EXISTS agent_commission_ledger (
    id BIGSERIAL PRIMARY KEY,
    agent_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    child_user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    request_id VARCHAR(128) NOT NULL,
    api_key_id BIGINT NOT NULL,
    group_id BIGINT NULL,
    model VARCHAR(255) NOT NULL DEFAULT '',
    base_cost DECIMAL(20,10) NOT NULL,
    child_rate DECIMAL(10,4) NOT NULL,
    agent_cost_rate DECIMAL(10,4) NOT NULL,
    child_paid DECIMAL(20,10) NOT NULL,
    agent_cost DECIMAL(20,10) NOT NULL,
    commission DECIMAL(20,10) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    frozen_until TIMESTAMPTZ NULL,
    reversal_of BIGINT NULL REFERENCES agent_commission_ledger(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT agent_commission_ledger_status_check CHECK (status IN ('pending', 'available', 'withdrawn', 'reversed'))
);

-- 与 usage_billing_dedup 同键：同一次用量结算重放不会重复产生佣金。
CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_commission_ledger_request
    ON agent_commission_ledger (request_id, api_key_id);

CREATE INDEX IF NOT EXISTS idx_agent_commission_ledger_agent_time
    ON agent_commission_ledger (agent_user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_agent_commission_ledger_child
    ON agent_commission_ledger (child_user_id, created_at DESC);

-- 只索引仍在冻结中的行，解冻任务扫描量与历史流水总量无关。
CREATE INDEX IF NOT EXISTS idx_agent_commission_ledger_pending_thaw
    ON agent_commission_ledger (frozen_until) WHERE status = 'pending';

COMMENT ON TABLE agent_commission_ledger IS '代理佣金逐笔流水：一次请求用量对应的差价佣金及其状态机';
COMMENT ON COLUMN agent_commission_ledger.agent_user_id IS '收佣代理（上级）的用户ID';
COMMENT ON COLUMN agent_commission_ledger.child_user_id IS '产生用量的下级用户ID；下级被删除后置 NULL，流水保留';
COMMENT ON COLUMN agent_commission_ledger.request_id IS '用量请求ID，与 usage_logs/usage_billing_dedup 的 request_id 同源，用于幂等';
COMMENT ON COLUMN agent_commission_ledger.api_key_id IS '产生用量的 API Key ID（不设外键：Key 删除后仍需保留佣金凭证）';
COMMENT ON COLUMN agent_commission_ledger.group_id IS '用量所属分组ID（不设外键：分组删除后仍需保留佣金凭证）';
COMMENT ON COLUMN agent_commission_ledger.model IS '用量命中的模型名（仅用于对账展示，不参与计算）';
COMMENT ON COLUMN agent_commission_ledger.base_cost IS '倍率前的基础成本（记账币种，TotalCost），与 usage_logs 口径一致';
COMMENT ON COLUMN agent_commission_ledger.child_rate IS '下级当次生效的售价倍率（相对基础成本的倍数）';
COMMENT ON COLUMN agent_commission_ledger.agent_cost_rate IS '代理当次生效的成本倍率（相对基础成本的倍数），无配置时为 1.0';
COMMENT ON COLUMN agent_commission_ledger.child_paid IS '下级实付金额（记账币种）= base_cost × child_rate';
COMMENT ON COLUMN agent_commission_ledger.agent_cost IS '代理结算成本（记账币种）= base_cost × agent_cost_rate';
COMMENT ON COLUMN agent_commission_ledger.commission IS '本笔佣金（记账币种）= child_paid - agent_cost，原则上非负';
COMMENT ON COLUMN agent_commission_ledger.status IS '佣金状态：pending（冻结中）|available（可提现）|withdrawn（已提现）|reversed（已冲正）';
COMMENT ON COLUMN agent_commission_ledger.frozen_until IS '冻结到期时间；status=pending 且该时间已过即可解冻，NULL 表示不冻结或已解冻';
COMMENT ON COLUMN agent_commission_ledger.reversal_of IS '冲正目标流水ID：本行是对该笔佣金的冲正记录（金额取负）';

-- 5) 佣金账户：每个代理一行，保存可提现/冻结/累计已提现三档余额。
--    用 upsert 维护而不是查改，所有增减都在一条语句里完成，避免读改写竞态。
CREATE TABLE IF NOT EXISTS agent_commission_accounts (
    agent_user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    available DECIMAL(20,8) NOT NULL DEFAULT 0,
    frozen DECIMAL(20,8) NOT NULL DEFAULT 0,
    withdrawn DECIMAL(20,8) NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT agent_commission_accounts_available_check CHECK (available >= 0),
    CONSTRAINT agent_commission_accounts_frozen_check CHECK (frozen >= 0),
    CONSTRAINT agent_commission_accounts_withdrawn_check CHECK (withdrawn >= 0)
);

COMMENT ON TABLE agent_commission_accounts IS '代理佣金账户：每个代理一行的可提现/冻结/累计已提现余额';
COMMENT ON COLUMN agent_commission_accounts.agent_user_id IS '代理的用户ID，同时是主键';
COMMENT ON COLUMN agent_commission_accounts.available IS '可提现佣金余额（记账币种，DECIMAL(20,8)，与 users.balance 同精度）';
COMMENT ON COLUMN agent_commission_accounts.frozen IS '冻结中的佣金余额（记账币种）；解冻时转入 available';
COMMENT ON COLUMN agent_commission_accounts.withdrawn IS '累计已提现佣金（记账币种），只增不减，用于对账';

-- 6) 提现登记：佣金提现走线下打款，这里只登记「谁、什么时候、提了多少、有没有打款」，
--    明细流水（钱从哪个余额扣）由 agent_commission_accounts 的增减体现。
--    operation_id 是调用方的幂等键：同一标识重复提交不会二次扣款。
CREATE TABLE IF NOT EXISTS agent_commission_withdrawals (
    id BIGSERIAL PRIMARY KEY,
    agent_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    amount DECIMAL(20,8) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'paid',
    operation_id VARCHAR(128) NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT agent_commission_withdrawals_amount_check CHECK (amount > 0),
    CONSTRAINT agent_commission_withdrawals_status_check CHECK (status IN ('paid', 'voided'))
);

-- 幂等键做成独立唯一索引而不是列内 UNIQUE：仓储用 `ON CONFLICT (operation_id)`
-- 推断冲突目标，显式命名索引比隐式约束名更好排查（也不会被 ALTER TABLE 改名弄丢）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_commission_withdrawals_operation
    ON agent_commission_withdrawals (operation_id);

CREATE INDEX IF NOT EXISTS idx_agent_commission_withdrawals_agent_time
    ON agent_commission_withdrawals (agent_user_id, created_at DESC);

COMMENT ON TABLE agent_commission_withdrawals IS '代理佣金提现登记：线下打款的提现凭证（不存支付渠道，只做账务留痕）';
COMMENT ON COLUMN agent_commission_withdrawals.agent_user_id IS '提现代理的用户ID';
COMMENT ON COLUMN agent_commission_withdrawals.amount IS '本次提现金额（记账币种，DECIMAL(20,8)，与可提现余额同精度）';
COMMENT ON COLUMN agent_commission_withdrawals.status IS '提现状态：paid（已打款）|voided（作废/退回）';
COMMENT ON COLUMN agent_commission_withdrawals.operation_id IS '调用方幂等键（同一标识只登记一次提现，重放返回首次结果）';
COMMENT ON COLUMN agent_commission_withdrawals.note IS '管理员备注（打款流水号、沟通记录等）';
