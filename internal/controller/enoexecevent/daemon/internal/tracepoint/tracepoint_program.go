package tracepoint

import (
	"fmt"

	"github.com/cilium/ebpf/asm"
)

const (
	ExitLabel           = "exit"
	CleanupLabel        = "cleanup"
	PayloadSize  uint32 = 8 // [bytes]
)

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

// https://www.kernel.org/doc/html/v5.17/bpf/instruction-set.html
// R0: return value from function calls, and exit value for eBPF programs
// R1 - R5: arguments for function calls
// R6 - R9: callee saved registers that function calls will preserve
// R10: read-only frame pointer to access stack

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

	tp.progSpec.Instructions = asm.Instructions{}
	for _, ins := range []asm.Instructions{
		// Registers mapping:
		// R6: current task's task_struct
		// R7: ring buffer event pointer
		// R8: real_parent task_struct
		jExitIfNoENOEXEC(),
		getCurrentTask(),
		// *** R6 = current task_struct
		ringBufReserve(tp.events.FD()),
		// *** R7 = ring buffer event pointer (reserved space)

		// Load the TGID of the current task (a 4 bytes word)
		// from R6 + tgidOffset into the ring buffer's second 4 bytes
		loadIntoRingBufEvent(asm.R6, *tp.tgidOffset, 4, 4),

		loadRealParent(*tp.realParentOffset),
		// *** R8 = real_parent task_struct pointer

		// Load the TGID of the real_parent task (a 4 bytes word)
		// from R8 + tgidOffset into the ring buffer's first 4 bytes
		loadIntoRingBufEvent(asm.R8, *tp.tgidOffset, 4, 0),

		submitEvent(),
		// Discard the reserved space in the ring buffer if submit failed.
		// rollbackEvent() is skipped if submit succeeded with a jump to exit().
		rollbackEvent(),
		// Exit the eBPF program with a return value of 0.
		exit(),
	} {
		tp.progSpec.Instructions = append(tp.progSpec.Instructions, ins...)
	}
	return nil
}

// jExitIfNoENOEXEC checks if the syscall return value is ENOEXEC (-8).
// if it is not, it jumps to the exit label.
// https://www.kernel.org/doc/man-pages/online/pages/man2/execve.2.html
func jExitIfNoENOEXEC() asm.Instructions {
	return asm.Instructions{
		asm.LoadMem(asm.R0, asm.R1, 16, asm.DWord),
		asm.JNE.Imm(asm.R0, -8, "exit"),
	}
}

// getCurrentTask retrieves the current task's task_struct pointer.
// The address of the current task_struct is stored in R6.
// https://docs.ebpf.io/linux/helper-function/bpf_get_current_task/
func getCurrentTask() asm.Instructions {
	return asm.Instructions{
		asm.FnGetCurrentTask.Call(),
		// Storing address of the current task_struct in R6
		asm.Mov.Reg(asm.R6, asm.R0),
	}
}

// ringBufReserve reserves space in the ring buffer for the event.
// It reserves space for the real_parent's TGID, current task's TGID.
// https://docs.ebpf.io/linux/helper-function/bpf_ringbuf_reserve/
// The reserved space is 16 bytes:
// 2 * sizeof(int32) [bytes]
// [ real_parent->tgid ][ current->tgid     ]
// [------4 bytes------][------4 bytes------]
func ringBufReserve(fd int) asm.Instructions {
	// R1: pointer to the ring buffer map
	// R2: size of the event to reserve (24 bytes)
	// R3: flags (must be 0)
	return asm.Instructions{
		asm.LoadMapPtr(asm.R1, fd),              // FD of ring buffer map
		asm.Mov.Imm(asm.R2, int32(PayloadSize)), // Size of the event to reserve (8 bytes)
		asm.Mov.Imm(asm.R3, 0),                  // Flags must be 0
		asm.FnRingbufReserve.Call(),             // Reserve space in the ring buffer
		asm.JEq.Imm(asm.R0, 0, ExitLabel),       // If reserve fails, exit
		asm.Mov.Reg(asm.R7, asm.R0),             // The address of the reserved space is stored in R7
	}
}

// loadIntoRingBufEvent loads data from the source pointer and stores it into the ring buffer event.
// It reads `size` bytes of data from the Kernel memory address at `srcReg` + `srcOffset`, and
// writes it to the ring buffer event using the address at R7 with `dstOffset` offset.
// srcReg is the register containing the source pointer (current task_struct or real_parent task_struct).
// srcOffset is the offset in the source pointer to read from.
// size is the number of bytes to read (4 bytes for TGID).
// dstOffset is the offset in the ring buffer event (R7) to write to.
// https://docs.ebpf.io/linux/helper-function/bpf_probe_read_kernel/
func loadIntoRingBufEvent(srcReg asm.Register, srcOffset, size, dstOffset int32) asm.Instructions {
	// R1: destination pointer (ring buffer)
	// R2: size of the data to read (4 bytes for TGID)
	// R3: source pointer (current task_struct)
	return asm.Instructions{
		asm.Mov.Reg(asm.R1, asm.R7),    // R7 is the ring buffer event pointer
		asm.Add.Imm(asm.R1, dstOffset), // Move to the destination offset in the ring buffer
		asm.Mov.Imm(asm.R2, size),      // Size of data to read
		asm.Mov.Reg(asm.R3, srcReg),    // Pointer to current task task_struct
		asm.Add.Imm(asm.R3, srcOffset), // Offset to the data to read in the source
		asm.FnProbeReadKernel.Call(),
		// jump to cleanup if bpf_probe_read_kernel failed
		asm.JNE.Imm(asm.R0, 0, CleanupLabel),
	}
}

// loadRealParent loads the real_parent pointer from the current task's task_struct.
// It reads the real_parent pointer from the task_struct and stores it in R8
func loadRealParent(offset int32) asm.Instructions {
	// Load real_parent pointer from task_struct
	// https://docs.ebpf.io/linux/helper-function/bpf_probe_read_kernel/
	return asm.Instructions{
		// Load real_parent pointer into the stack
		// R1: destination pointer (stack)
		// R2: size of the data to read (8 bytes for pointer)
		// R3: source pointer (current task_struct)
		asm.Mov.Reg(asm.R1, asm.R10), // R10 is the frame pointer to access the stack. dst for bpf_probe_read_kernel
		asm.Add.Imm(asm.R1, -8),      // offset to the last 8 bytes of the stack
		asm.Mov.Imm(asm.R2, 8),       // sizeof pointer (__u32 size)
		asm.Mov.Reg(asm.R3, asm.R6),  // pointer to current task task_struct (unsafe_ptr)
		asm.Add.Imm(asm.R3, offset),  // offset to real_parent
		asm.FnProbeReadKernel.Call(),
		// jump to cleanup if bpf_probe_read_kernel failed
		asm.JNE.Imm(asm.R0, 0, CleanupLabel),
		// Load real_parent pointer from stack into R8
		asm.LoadMem(asm.R8, asm.R10, -8, asm.DWord),
		// safety check real_parent != NULL
		asm.JEq.Imm(asm.R8, 0, CleanupLabel),
	}
}

// submitEvent submits the event to the ring buffer. This is where the event is sent to the user space
// after it has been reserved and populated with data from the current task and its real parent.
func submitEvent() asm.Instructions {
	// R1: ring buffer event pointer (R7)
	// R2: flags (must be 0)
	return asm.Instructions{
		asm.Mov.Reg(asm.R1, asm.R7),
		asm.Mov.Imm(asm.R2, 0),
		asm.FnRingbufSubmit.Call(),
		asm.Ja.Label(ExitLabel), // jump past cleanup if success
	}
}

// rollbackEvent is used to discard the reserved space in the ring buffer when any error occurs while
// running the eBPF program after the event space has been reserved.
func rollbackEvent() asm.Instructions {
	// If the event submission failed, we need to discard the reserved space in the ring buffer.
	// R1: ring buffer event pointer (R7)
	// R2: flags (must be 0)
	return asm.Instructions{
		asm.Mov.Reg(asm.R1, asm.R7).WithSymbol(CleanupLabel),
		asm.Mov.Imm(asm.R2, 0),
		asm.FnRingbufDiscard.Call(),
	}
}

// exit exits the eBPF program with a return value of 0.
func exit() asm.Instructions {
	return asm.Instructions{
		asm.Mov.Imm(asm.R0, 0).WithSymbol(ExitLabel), // Set return value to 0
		asm.Return(),
	}
}
