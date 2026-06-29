package pdpv0

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/tasks/tasknames"
)

func TestDeleteDataSetMayFollowProveAndProvPeriod(t *testing.T) {
	td := (&DeleteDataSetTask{}).TypeDetails()
	require.Equal(t, tasknames.PDPv0_DelDataSet, td.Name)
	require.Contains(t, td.MayFollow, tasknames.PDPv0_Prove)
	require.Contains(t, td.MayFollow, tasknames.PDPv0_ProvPeriod,
		"delete must wait for prove and proving-period tasks to finish")
}
