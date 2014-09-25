package engine

import (
	"github.com/dropbox/changes-client/adapter/basic"
	"github.com/dropbox/changes-client/client"
	"github.com/dropbox/changes-client/common"
	"github.com/dropbox/changes-client/reporter"
	"log"
	"os"
	"sync"
)

const (
	STATUS_QUEUED      = "queued"
	STATUS_IN_PROGRESS = "in_progress"
	STATUS_FINISHED    = "finished"

	RESULT_PASSED = "passed"
	RESULT_FAILED = "failed"
)

func RunAllCmds(reporter *reporter.Reporter, config *client.Config, logsource *reporter.LogSource) string {
	var err error

	result := RESULT_PASSED

	adapter := basic.NewAdapter(config)
	log := client.NewLog()

	wg := sync.WaitGroup{}

	wg.Add(1)
	go func() {
		logsource.ReportChunks(log.Chan)
		wg.Done()
	}()

	err = adapter.Prepare(log)
	if err != nil {
		// TODO(dcramer): we need to ensure that logging gets generated for prepare
		return RESULT_FAILED
	}

	for _, cmdConfig := range config.Cmds {
		cmd, err := client.NewCommand(cmdConfig.ID, cmdConfig.Script)
		if err != nil {
			reporter.PushStatus(cmd.ID, STATUS_FINISHED, 255)
			result = RESULT_FAILED
			break
		}
		reporter.PushStatus(cmd.ID, STATUS_IN_PROGRESS, -1)

		cmd.CaptureOutput = cmdConfig.CaptureOutput

		env := os.Environ()
		for k, v := range cmdConfig.Env {
			env = append(env, k+"="+v)
		}
		cmd.Env = env

		if len(cmdConfig.Cwd) > 0 {
			cmd.Cwd = cmdConfig.Cwd
		}

		cmdResult, err := adapter.Run(cmd, log)

		if err != nil {
			reporter.PushStatus(cmd.ID, STATUS_FINISHED, 255)
			result = RESULT_FAILED
		} else {
			if cmdResult.Success() {
				if cmd.CaptureOutput {
					reporter.PushOutput(cmd.ID, STATUS_FINISHED, 0, cmdResult.Output)
				} else {
					reporter.PushStatus(cmd.ID, STATUS_FINISHED, 0)
				}
			} else {
				reporter.PushStatus(cmd.ID, STATUS_FINISHED, 1)
				result = RESULT_FAILED
			}
		}

		wg.Add(1)
		go func(artifacts []string) {
			publishArtifacts(reporter, config.Workspace, artifacts)
			wg.Done()
		}(cmdConfig.Artifacts)

		if result == RESULT_FAILED {
			break
		}
	}

	err = adapter.Shutdown(log)

	close(log.Chan)

	wg.Wait()

	if err != nil {
		// TODO(dcramer): we need to ensure that logging gets generated for prepare
		// XXX(dcramer): we probably don't need to fail here as a shutdown operation
		// should be recoverable
		return RESULT_FAILED
	}

	return result
}

func RunBuildPlan(r *reporter.Reporter, config *client.Config) {
	logsource := reporter.NewLogSource("console", r)

	r.PushJobStatus(STATUS_IN_PROGRESS, "")

	result := RunAllCmds(r, config, logsource)

	r.PushJobStatus(STATUS_FINISHED, result)
}

func publishArtifacts(r *reporter.Reporter, workspace string, artifacts []string) {
	if len(artifacts) == 0 {
		log.Printf("[engine] Skipping artifact collection")
		return
	}

	log.Printf("[engine] Collecting artifacts in %s matching %s", workspace, artifacts)

	matches, err := common.GlobTree(workspace, artifacts)
	if err != nil {
		panic("Invalid artifact pattern" + err.Error())
	}

	log.Printf("[engine] Found %d matching artifacts", len(matches))

	r.PushArtifacts(matches)
}
