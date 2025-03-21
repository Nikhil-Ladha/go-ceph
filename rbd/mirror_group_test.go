package rbd

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupMirroring(t *testing.T) {
	mconfig := mirrorConfig()
	if mconfig == "" {
		t.Skip("no mirror config env var set")
	}

	conn := radosConnect(t)
	poolName := GetUUID()
	err := conn.MakePool(poolName)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, conn.DeletePool(poolName))
		conn.Shutdown()
	}()

	ioctx, err := conn.OpenIOContext(poolName)
	assert.NoError(t, err)
	defer func() {
		ioctx.Destroy()
	}()

	// enable per-image mirroring for this pool
	err = SetMirrorMode(ioctx, MirrorModeImage)
	require.NoError(t, err)

	name := GetUUID()
	options := NewRbdImageOptions()
	assert.NoError(t,
		options.SetUint64(ImageOptionOrder, uint64(testImageOrder)))
	err = CreateImage(ioctx, name, testImageSize, options)
	require.NoError(t, err)

	groupName := "group1"
	err = GroupCreate(ioctx, groupName)
	assert.NoError(t, err)

	err = GroupImageAdd(ioctx, groupName, ioctx, name)
	assert.NoError(t, err)

	token, err := CreateMirrorPeerBootstrapToken(ioctx)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(token), 4)

	conn2 := radosConnectConfig(t, mconfig)
	defer conn2.Shutdown()

	err = conn2.MakePool(poolName)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, conn2.DeletePool(poolName))
	}()

	ioctx2, err := conn2.OpenIOContext(poolName)
	assert.NoError(t, err)
	defer func() {
		ioctx2.Destroy()
	}()

	err = SetMirrorMode(ioctx2, MirrorModeImage)
	require.NoError(t, err)

	err = ImportMirrorPeerBootstrapToken(
		ioctx2, MirrorPeerDirectionRxTx, token)
	assert.NoError(t, err)

	// enable mirroring
	err = MirrorGroupEnable(ioctx, groupName, ImageMirrorModeSnapshot)
	assert.NoError(t, err)

	waitCounter := 30
	// wait for mirroring to be enabled
	for i := 0; i < waitCounter; i++ {
		resp, err := GetMirrorGroupInfo(ioctx, groupName)
		assert.NoError(t, err)
		if resp.State.String() == "enabled" {
			break
		}
		if i == waitCounter-1 {
			assert.Fail(t, "mirror not enabled")
		}
		time.Sleep(2 * time.Second)
	}

	// verify mirror group status on primary
	for i := 0; i < waitCounter; i++ {
		resp, err := GetGlobalMirrorGroupStatus(ioctx, groupName)
		assert.NoError(t, err)
		if resp.SiteStatusesCount > 0 {
			break
		}
		localStatus, _ := resp.LocalStatus()
		if localStatus.State.String() == "stopped" {
			break
		}
		time.Sleep(2 * time.Second)
	}

	// verify mirror group status on secondary
	for i := 0; i < waitCounter; i++ {
		resp, err := GetGlobalMirrorGroupStatus(ioctx2, groupName)
		assert.NoError(t, err)
		if resp.SiteStatusesCount > 0 {
			break
		}
		localStatus, _ := resp.LocalStatus()
		if localStatus.State.String() == "replaying" {
			break
		}
		time.Sleep(2 * time.Second)
	}

	// demote primary and promote secondary mirror group, i.e, relocate
	err = MirrorGroupDemote(ioctx, groupName)
	assert.NoError(t, err)

	// wait for peer mirror group on primary to be secondary
	for i := 0; i < 30; i++ {
		resp, err := GetMirrorGroupInfo(ioctx, groupName)
		assert.NoError(t, err)
		if !resp.Primary {
			break
		}
		if i == waitCounter-1 {
			assert.Fail(t, "mirror group on secondary site is not demoted")
		}
		time.Sleep(2 * time.Second)
	}

	resp1, err := GetGlobalMirrorGroupStatus(ioctx, groupName)
	resp2, err := GetGlobalMirrorGroupStatus(ioctx2, groupName)
	localStatus1, _ := resp1.LocalStatus()
	localStatus2, _ := resp2.LocalStatus()

	// wait for images to be synced, i.e, wait for up+unkown state on both the clusters
	for i := 0; i < 30; i++ {
		if localStatus1.State.String() == "unknown" {
			break
		}
		if i == waitCounter-1 {
			assert.Fail(t, "mirror group on new secondary site is not yet synced")
		}
		time.Sleep(2 * time.Second)
	}

	// wait for images to be synced, i.e, wait for up+unkown state on both the clusters
	for i := 0; i < 30; i++ {
		if localStatus2.State.String() == "unknown" {
			break
		}
		if i == waitCounter-1 {
			assert.Fail(t, "mirror group on old secondary site has not completed syncing")
		}
		time.Sleep(2 * time.Second)
	}

	// promote mirror group
	err = MirrorGroupPromote(ioctx2, groupName, true)
	assert.NoError(t, err)

	// wait for mirror group to be promoted
	for i := 0; i < waitCounter; i++ {
		resp, err := GetMirrorGroupInfo(ioctx2, groupName)
		assert.NoError(t, err)
		if resp.Primary {
			break
		}
		if i == waitCounter-1 {
			assert.Fail(t, "mirror group on new Primary site is not promoted")
		}
		time.Sleep(2 * time.Second)
	}
}
