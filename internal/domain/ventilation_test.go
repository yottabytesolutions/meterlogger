package domain

import "testing"

func TestNodeDevType(t *testing.T) {
	base := BaseDucoNodeStatus{DevType: "BOX"}
	if base.NodeDevType() != "BOX" {
		t.Errorf("NodeDevType() = %q, want BOX", base.NodeDevType())
	}
}

func TestDucoNodeStatus_Interface(_ *testing.T) {
	// Verify all concrete types implement DucoNodeStatus.
	var _ DucoNodeStatus = DucoNodeBoxStatus{}
	var _ DucoNodeStatus = DucoNodeBoxValveStatus{}
	var _ DucoNodeStatus = DucoRFSensorStatus{}
}
