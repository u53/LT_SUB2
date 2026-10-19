//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// agentDistributionEnabledOrDefault 的默认值语义：未提供时开启。
// 存量分组的 JSON 里没有该字段，若默认成 false 会让所有既有分组停止计佣。
func TestAgentDistributionEnabledOrDefault(t *testing.T) {
	require.True(t, agentDistributionEnabledOrDefault(nil), "未提供时必须默认开启")

	disabled := false
	require.False(t, agentDistributionEnabledOrDefault(&disabled))

	enabled := true
	require.True(t, agentDistributionEnabledOrDefault(&enabled))
}

// 分组关闭分销后，计佣入口必须直接短路；这里验证分组字段本身可被正确读写，
// 计费链路的短路依赖它。
func TestGroup_AgentDistributionEnabledField(t *testing.T) {
	group := &Group{RateMultiplier: 1}
	require.False(t, group.AgentDistributionEnabled, "零值分组结构体默认关闭，用于测试桩显式设置")

	group.AgentDistributionEnabled = true
	require.True(t, group.AgentDistributionEnabled)
}
