package compute

// GPUMemory is one GPU's memory as the observer reports it, in bytes. A zero
// TotalBytes means the observer reported no memory for that GPU.
type GPUMemory struct {
	TotalBytes uint64
	UsedBytes  uint64
}

// NodeObservation is the observer data a node's slot count reads. GPUs holds
// one entry per GPU the observer reports; a node with none has no observer
// data for slots. LoadedModelBytes holds the VRAM each model the node has
// loaded now takes. LastModelBytes is the size of the model the node served
// the last time one was loaded; the caller keeps it across reads, because a
// model unloads when idle. MaxUtilPct is the busiest GPU's utilisation, which
// the GPU backpressure gate reads.
type NodeObservation struct {
	GPUs             []GPUMemory
	LoadedModelBytes []uint64
	LastModelBytes   uint64
	MaxUtilPct       float64
}

// NodeSlots returns how many workers a compute node takes at once.
//
// override is the operator's session_slots value; above 0 it wins. Without
// one, a node with observer GPU data gets one slot per GPU, limited by the
// free GPU memory against the size of the model the node serves (see
// derivedSlots). A node with no observer GPU data takes fallback, which is
// autonomous.max_engines_per_node.
//
// held is the number of workers holding a lease on the node now. A derived
// count never falls below it, so workers already running are not counted out
// when free memory shrinks. The floor is capped at the GPU count, so a
// single-GPU node stays at one slot. A derived count is never below 1, because
// a pool limit of 0 means unlimited to the capacity ledger. The operator value
// is not floored by held: it is the figure the operator asked for.
//
// Known gap: a node that runs more workers than it has GPUs reports a limit
// below its held count, so the workers above the GPU count are not protected by
// the floor. The ledger does not evict a lease when a limit drops; the workers
// keep running and new work waits until the held count falls below the limit.
//
// ceiling is the node's max_concurrent_sessions; when above 0 it caps the
// result.
func NodeSlots(override, ceiling, fallback, held int, obs NodeObservation) int {
	var n int
	switch {
	case override > 0:
		n = override
	case len(obs.GPUs) == 0:
		n = max(fallback, 1)
	default:
		n = max(derivedSlots(obs), min(held, len(obs.GPUs)), 1)
	}
	if ceiling > 0 && n > ceiling {
		n = ceiling
	}
	return n
}

// derivedSlots is one slot per GPU, limited by memory. The model size is the
// largest loaded model; with none loaded it is the size the node last served
// (LastModelBytes). The free memory is each GPU's total minus its used memory,
// so memory in use by any program, the node's own loaded model included, is
// taken out. The count is the number of whole models that free memory holds,
// capped at the GPU count.
//
// When no model size is known at all, or when any GPU reports no total memory,
// the memory limit cannot be applied and the count is one slot per GPU.
// Observer data does not place each loaded model on a GPU, so the free memory
// is summed over all GPUs: a node can read as having more room than its free
// GPU happens to hold.
func derivedSlots(obs NodeObservation) int {
	gpus := len(obs.GPUs)
	var model uint64
	for _, b := range obs.LoadedModelBytes {
		model = max(model, b)
	}
	if model == 0 {
		model = obs.LastModelBytes
	}
	if model == 0 {
		return gpus
	}
	var free uint64
	for _, g := range obs.GPUs {
		if g.TotalBytes == 0 {
			return gpus
		}
		if g.UsedBytes < g.TotalBytes {
			free += g.TotalBytes - g.UsedBytes
		}
	}
	return min(gpus, int(free/model))
}
