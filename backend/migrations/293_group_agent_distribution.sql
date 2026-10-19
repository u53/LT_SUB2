-- 分组是否允许代理分销。
-- 关闭后该分组的用量不产生代理佣金；存量分组默认开启，行为不变。
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS agent_distribution_enabled BOOLEAN NOT NULL DEFAULT TRUE;

COMMENT ON COLUMN groups.agent_distribution_enabled IS
    '是否允许代理分销：关闭后该分组的用量不产生佣金';
