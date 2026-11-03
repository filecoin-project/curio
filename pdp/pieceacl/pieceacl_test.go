package pieceacl

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnyEveryPublic(t *testing.T) {
	secured := Link{ID: 1, ACL: sql.NullInt32{Int32: Secured, Valid: true}}
	public0 := Link{ID: 2, ACL: sql.NullInt32{Int32: 0, Valid: true}}
	unresolved := Link{ID: 3, ACL: sql.NullInt32{Valid: false}}

	require.False(t, AnySecured(nil))
	require.False(t, EverySecured(nil))
	require.False(t, AnyPublic(nil))

	require.True(t, AnySecured([]Link{secured}))
	require.True(t, EverySecured([]Link{secured, secured}))
	require.False(t, EverySecured([]Link{secured, public0}))
	require.False(t, EverySecured([]Link{secured, unresolved}))

	require.True(t, AnyPublic([]Link{public0}))
	require.True(t, AnyPublic([]Link{unresolved}))
	require.True(t, AnyPublic([]Link{secured, public0}))
	require.False(t, AnyPublic([]Link{secured}))
}
