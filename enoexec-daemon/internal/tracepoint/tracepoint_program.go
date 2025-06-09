package tracepoint

import (
	"fmt"

	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/btf"
)

func (tp *Tracepoint) initializeOffsets() error {
	spec, err := btf.LoadKernelSpec()
	if err != nil {
		return fmt.Errorf("failed to load kernel BTF spec: %w", err)
	}
	taskStructIf, err := spec.AnyTypeByName("task_struct")
	if err != nil {
		return fmt.Errorf("failed to find task_struct: %w", err)
	}

	taskStruct, ok := taskStructIf.(*btf.Struct)
	if !ok {
		return fmt.Errorf("task_struct is not a struct")
	}
	var realParentOffset, tgidOffset int32
	var realParentFound, tgidFound bool
	for _, member := range taskStruct.Members {
		if member.Name == "real_parent" {
			realParentOffset = int32(member.Offset.Bytes())
			realParentFound = true
		}
		if member.Name == "tgid" {
			tgidOffset = int32(member.Offset.Bytes())
			tgidFound = true
		}
	}
	if !realParentFound || !tgidFound {
		return fmt.Errorf("failed to find real_parent or tgid in task_struct")
	}
	tp.realParentOffset = &realParentOffset
	tp.tgidOffset = &tgidOffset
	return nil
}

// initializeProgSpec initializes the eBPF program for the tracepoint.
// It needs to be called after the offsets are set.
// It needs to be called after the events map is created.
// It needs to be called before the program is loaded.
func (tp *Tracepoint) initializeProgSpec() error {
	if tp.events.FD() == 0 {
		return fmt.Errorf("events map FD is not set")
	}
	if tp.tgidOffset == nil || tp.realParentOffset == nil {
		return fmt.Errorf("tgidOffset or realParentOffset is not set")
	}

	// https://stackoverflow.com/questions/9305992/if-threads-share-the-same-pid-how-can-they-be-identified
	//                          USER VIEW
	//                         vvvv vvvv
	//              |
	//<-- PID 43 -->|<----------------- PID 42 ----------------->
	//              |                           |
	//              |      +---------+          |
	//              |      | process |          |
	//              |     _| pid=42  |_         |
	//         __(fork) _/ | tgid=42 | \_ (new thread) _
	//        /     |      +---------+          |       \
	//+---------+   |                           |    +---------+
	//| process |   |                           |    | process |
	//| pid=43  |   |                           |    | pid=44  |
	//| tgid=43 |   |                           |    | tgid=42 |
	//+---------+   |                           |    +---------+
	//              |                           |
	//<-- PID 43 -->|<--------- PID 42 -------->|<--- PID 44 --->
	//              |                           |
	//                        ^^^^^^ ^^^^
	//                        KERNEL VIEW

	tp.progSpec.Instructions = asm.Instructions{
		// https://www.kernel.org/doc/html/v5.17/bpf/instruction-set.html
		// R0: return value from function calls, and exit value for eBPF programs
		// R1 - R5: arguments for function calls
		// R6 - R9: callee saved registers that function calls will preserve
		// R10: read-only frame pointer to access stack

		// https://www.kernel.org/doc/html/latest/trace/events.html
		// Load syscall return value (args->ret)
		// └ # cat /sys/kernel/debug/tracing/events/syscalls/sys_exit_execve/format
		//  name: sys_exit_execve
		//  ID: 869
		//  format:
		//        field:unsigned short common_type;       offset:0;       size:2; signed:0;
		//        field:unsigned char common_flags;       offset:2;       size:1; signed:0;
		//        field:unsigned char common_preempt_count;       offset:3;       size:1; signed:0;
		//        field:int common_pid;   offset:4;       size:4; signed:1;
		//
		//        field:int __syscall_nr; offset:8;       size:4; signed:1;
		//        field:long ret; offset:16;      size:8; signed:1;
		//                                V
		//print fmt: "0x%lx", REC->ret    V

		// Registers mapping:
		// R6: current task's task_struct
		// R7: ring buffer event pointer
		// R8: real_parent task_struct

		// Verify that the syscall returned ENOEXEC (-8) or jump to exit
		asm.LoadMem(asm.R0, asm.R1, 16, asm.DWord),
		asm.JNE.Imm(asm.R0, -8, "exit"),

		// Get current task_struct
		// https://docs.ebpf.io/linux/helper-function/bpf_get_current_task/
		asm.FnGetCurrentTask.Call(),
		asm.Mov.Reg(asm.R6, asm.R0), // Storing address of the current task_struct in R6

		// Reserve ring buffer event (sizeof(int32) * 2)
		// [------4 bytes------][------4 bytes------]
		// [ real_parent->tgid ][ current->tgid     ]
		// https://docs.ebpf.io/linux/helper-function/bpf_ringbuf_reserve/
		asm.LoadMapPtr(asm.R1, tp.events.FD()), // FD of ring buffer map
		asm.Mov.Imm(asm.R2, 4*2),               // sizeof(int32) * 2
		asm.Mov.Imm(asm.R3, 0),                 // flags must be 0
		asm.FnRingbufReserve.Call(),
		asm.JEq.Imm(asm.R0, 0, "exit"), // if reserve fails, exit
		asm.Mov.Reg(asm.R7, asm.R0),    // store reserved ptr in R7

		// Load current->tgid into the last 4 bytes of the ring buffer's new reserved item
		// https://docs.ebpf.io/linux/helper-function/bpf_probe_read_kernel/
		asm.Mov.Reg(asm.R1, asm.R7),         // ringbuf ptr
		asm.Add.Imm(asm.R1, 4),              // offset to the last 4 bytes
		asm.Mov.Imm(asm.R2, 4),              // sizeof TGID
		asm.Mov.Reg(asm.R3, asm.R6),         // pointer to current task task_struct
		asm.Add.Imm(asm.R3, *tp.tgidOffset), // offset to tgid
		asm.FnProbeReadKernel.Call(),

		// Load real_parent pointer into the stack
		asm.Mov.Reg(asm.R1, asm.R10),              // R10 is the frame pointer to access stack. dst for bpf_probe_read_kernel
		asm.Add.Imm(asm.R1, -8),                   // temporary stack space
		asm.Mov.Imm(asm.R2, 8),                    // sizeof pointer (__u32 size)
		asm.Mov.Reg(asm.R3, asm.R6),               // pointer to current task task_struct (unsafe_ptr)
		asm.Add.Imm(asm.R3, *tp.realParentOffset), // offset to real_parent
		asm.FnProbeReadKernel.Call(),

		// Load real_parent pointer from stack into R8
		asm.LoadMem(asm.R8, asm.R10, -8, asm.DWord),
		asm.JEq.Imm(asm.R8, 0, "cleanup"), // safety check real_parent != NULL

		// Safely load the real_parent's TGID into the first 4 bytes of the ring buffer's new reserved item
		asm.Mov.Reg(asm.R1, asm.R7),         // ringbuf ptr
		asm.Mov.Imm(asm.R2, 4),              // sizeof TGID
		asm.Mov.Reg(asm.R3, asm.R8),         // real_parent pointer
		asm.Add.Imm(asm.R3, *tp.tgidOffset), // offset to tgid
		asm.FnProbeReadKernel.Call(),

		// Submit ring buffer event
		asm.Mov.Reg(asm.R1, asm.R7),
		asm.Mov.Imm(asm.R2, 0),
		asm.FnRingbufSubmit.Call(),

		asm.Ja.Label("exit"), // jump past cleanup if success

		// cleanup if error
		asm.Mov.Reg(asm.R1, asm.R7).WithSymbol("cleanup"),
		asm.Mov.Imm(asm.R2, 0),
		asm.FnRingbufDiscard.Call(),

		// exit
		asm.Mov.Imm(asm.R0, 0).WithSymbol("exit"),
		asm.Return(),
	}
	return nil
}
