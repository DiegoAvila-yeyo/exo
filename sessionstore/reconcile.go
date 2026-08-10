package sessionstore

import "github.com/DiegoAvila-yeyo/exo/realpty"

type ReconcileResult struct {
	SessionID string
	Status    string
}

func Reconcile(store *Store, currentInstanceID string) ([]ReconcileResult, error) {
	records, err := store.ListUnreconciled(currentInstanceID)
	if err != nil {
		return nil, err
	}

	results := make([]ReconcileResult, 0, len(records))
	for _, record := range records {
		status := reconcileOne(record)
		if err := store.MarkReconciled(record.SessionID, status); err != nil {
			return results, err
		}
		results = append(results, ReconcileResult{
			SessionID: record.SessionID,
			Status:    status,
		})
	}
	return results, nil
}

func reconcileOne(record SessionMetadata) string {
	if !realpty.ProcessGroupAlive(record.ProcessGroupID) {
		return StatusStaleReaped
	}

	snapshot, err := realpty.ProcessSnapshot(record.ShellPID)
	if err != nil {
		if !realpty.ProcessGroupAlive(record.ProcessGroupID) {
			return StatusStaleReaped
		}
		if err := realpty.TerminateProcessGroup(record.ProcessGroupID); err != nil {
			return StatusStaleOrphaned
		}
		if realpty.ProcessGroupAlive(record.ProcessGroupID) {
			return StatusStaleOrphaned
		}
		return StatusStaleReaped
	}
	if snapshot.PGID != record.ProcessGroupID || snapshot.StartTime != record.ShellStartTime {
		return StatusStaleOrphaned
	}
	if err := realpty.TerminateProcessTree(record.ShellPID, record.ProcessGroupID); err != nil {
		return StatusStaleOrphaned
	}
	if realpty.ProcessGroupAlive(record.ProcessGroupID) {
		return StatusStaleOrphaned
	}
	return StatusStaleReaped
}
