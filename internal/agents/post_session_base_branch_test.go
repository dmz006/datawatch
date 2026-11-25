package agents

import "testing"

// Finished work is never pushed onto a main branch by the pull request hook.
func TestPostSessionPRHook_NeverPushesABaseBranch(t *testing.T) {
	for _, branch := range []string{"main", "master"} {
		pusher := &fakePusher{branchToReturn: branch}
		prov := &recordingProvider{}
		hook, _, agentID := hookFixture(t, true, pusher, prov)
		hook(&fakeSession{id: "s1", agent: agentID, dir: "/work/repo", task: "do x"})
		if len(pusher.pushCalls) != 0 || len(prov.openPRCalls) != 0 {
			t.Errorf("%s: pushed %v, opened %d", branch, pusher.pushCalls, len(prov.openPRCalls))
		}
	}
}
