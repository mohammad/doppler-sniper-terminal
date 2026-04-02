package app

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
)

type RuntimeStatus struct {
	RemoteMode        bool
	SourceLabel       string
	DatabaseReachable bool
	SchemaReady       bool
	CheckpointCount   int
	IndexerReady      bool
	LatestSwapTS      int64
	IndexerRunning    bool
	IndexerCommand    string
}

func (s *Service) InspectRuntimeStatus(ctx context.Context) RuntimeStatus {
	status := RuntimeStatus{
		RemoteMode:  s.remote,
		SourceLabel: s.sourceLabel,
	}

	if err := s.repo.Ping(ctx); err == nil {
		status.DatabaseReachable = true
		if ready, err := s.repo.HasRequiredSchema(ctx); err == nil {
			status.SchemaReady = ready
		}
		if checkpoints, err := s.repo.CountCheckpoints(ctx); err == nil {
			status.CheckpointCount = checkpoints
		}
		if ready, err := s.repo.IsIndexerReady(ctx); err == nil {
			status.IndexerReady = ready
		}
		if latest, err := s.repo.GetLatestSwapTimestamp(ctx); err == nil {
			status.LatestSwapTS = latest
		}
	}

	if !s.remote {
		status.IndexerRunning, status.IndexerCommand = detectIndexerProcess(ctx)
	}
	return status
}

func detectIndexerProcess(ctx context.Context) (bool, string) {
	output, err := exec.CommandContext(ctx, "ps", "-Ao", "command=").Output()
	if err != nil {
		return false, ""
	}

	indexerDir := filepath.Clean("../doppler-indexer")
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "ponder") && !strings.Contains(lower, "pnpm run dev") && !strings.Contains(lower, "pnpm run start") {
			continue
		}
		if strings.Contains(lower, "ponder.config") || strings.Contains(lower, "doppler-indexer") || strings.Contains(line, indexerDir) {
			return true, strings.TrimSpace(line)
		}
	}

	return false, ""
}
