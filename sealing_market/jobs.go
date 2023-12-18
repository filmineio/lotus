package sealing_market

type Jobs interface {
	*Pr2Input // *Pr2Input | *C2JobRequest| *C2JobRequest
	JobType() string
}

type AddJobRequest[T Jobs] struct {
	JobType string `json:"job_type"`
	Input   T      `json:"input"`
}

type Pr2Input struct {
	VannilaProofs     [][]byte `json:"vanilla_proofs"`
	CommRNew          string   `json:"comm_r_new"`
	CommROld          string   `json:"comm_r_old"` // cid.Cid
	CommDNew          string   `json:"comm_d_new"`
	StorageProviderId uint64   `json:"storage_provider_id"`
	SectorId          uint64   `json:"sector_id"`
	RegisteredProof   string   `json:"registered_proof"`
}

func (j Pr2Input) JobType() string { return "PR2" }

type Output[T any] struct {
	JobType string `json:"job_type"`
	Output  T      `json:"output"`
}
