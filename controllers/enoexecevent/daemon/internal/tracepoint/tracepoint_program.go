package tracepoint

import (
	"fmt"

	"github.com/cilium/ebpf/asm"
)

// initializeProgSpec initializes the eBPF program for the tracepoint.
// It must be called after the offsets are set.
// It must be called after the events map is created.
// It must be called before the program is loaded.
func (tp *Tracepoint) initializeProgSpec() error {
	if tp.events.FD() == 0 {
		return fmt.Errorf("events map FD is not set")
	}
	if tp.tgidOffset == nil || tp.realParentOffset == nil {
		return fmt.Errorf("tgidOffset or realParentOffset is not set")
	}

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
		//        field:unsigned short common_type;       	offset:0;       size:2; signed:0;
		//        field:unsigned char common_flags;       	offset:2;       size:1; signed:0;
		//        field:unsigned char common_preempt_count; offset:3;       size:1; signed:0;
		//        field:int common_pid;   					offset:4;       size:4; signed:1;
		//
		//        field:int __syscall_nr; 					offset:8;       size:4; signed:1;
		//        field:long ret; 							offset:16;      size:8; signed:1;
		//                                V
		//print fmt: "0x%lx", REC->ret    V

		// Registers mapping:
		// R6: current task's task_struct
		// R7: ring buffer event pointer
		// R8: real_parent task_struct

		// Verify that the syscall returned ENOEXEC (-8) or jump to exit
		asm.LoadMem(asm.R0, asm.R1, 16, asm.DWord),
		asm.JNE.Imm(asm.R0, -8, "exit"),

		// TODO: implement the logic to handle the event

		// exit
		asm.Mov.Imm(asm.R0, 0).WithSymbol("exit"),
		asm.Return(),
	}
	return nil
}
