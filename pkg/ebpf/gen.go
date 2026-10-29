package ebpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target bpfel,bpfeb -type agent_cfg -type target_proc -type ds_key -type ds_target -type orig_dst -type ing_data_event -type conn_observe_event cros_probe bpf/cros_probe.c -- -I/usr/include -I/usr/include/x86_64-linux-gnu
