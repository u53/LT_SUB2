-- 价格条目币种：让国产模型直接录入人民币官网价，海外模型保留美元官网价。
-- 空值表示与记账币种（billing.accounting_currency）相同，因此存量条目语义不变。
ALTER TABLE channel_model_pricing
    ADD COLUMN IF NOT EXISTS currency VARCHAR(3) NOT NULL DEFAULT '';

COMMENT ON COLUMN channel_model_pricing.currency IS
    '价格录入币种（ISO 4217）；空值表示与记账币种相同。计费前按 billing.currency_rates 折成记账币种。';
