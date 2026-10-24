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
// loaded now takes. MaxUtilPct is the busiest GPU's utilisation, which the
// GPU backpressure gate reads.
type NodeObservation struct {
	GPUs             []GPUMemory
	LoadedModelBytes []uint64
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
// when free memory shrinks. A derived count is never below 1, because a pool
// limit of 0 means unlimited to the capacity ledger. The operator value is not
// floored by held: it is the figure the operator asked for.
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
		n = max(derivedSlots(obs), held, 1)
	}
	if ceiling > 0 && n > ceiling {
		n = ceiling
	}
	return n
}

// derivedSlots is one slot per GPU, limited by memory. The model the node
// serves is its largest loaded model. The free memory is the GPUs' total minus
// their used memory, plus the VRAM the loaded models hold: that memory already
// serves this node, so it is not counted as taken by another program. The
// count is the number of whole models that free memory holds, capped at the
// GPU count.
//
// Without a loaded model, or when any GPU reports no total memory, the memory
// limit cannot be applied and the count is one slot per GPU. Observer data
// does not place each loaded model on a GPU, so the free memory is summed over
// all GPUs: a node can read as having more room than its free GPU happens to
// hold.
func derivedSlots(obs NodeObservation) int {
	gpus := len(obs.GPUs)
	var model, loaded uint64
	for _, b := range obs.LoadedModelBytes {
		model = max(model, b)
		loaded += b
	}
	if model == 0 {
		return gpus
	}
	free := loaded
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
