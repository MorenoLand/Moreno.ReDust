// DumpGateDecoder.java: reads the packed walk-gate decoder at FUN_00427580 for the DUST
// program that the headless run opens read-only. It prints the dword table the decoder
// jumps through (base 0x00427619, index 2*b), disassembles each table value that lands in
// the image, and decompiles the functions those values reach. Nothing is saved back.
//
// Usage (read-only; the program is not saved):
//   analyzeHeadless <project_dir> <project_name> -process DF386.EXE -noanalysis -readOnly \
//       -scriptPath <tools/ghidra> -postScript DumpGateDecoder.java
import ghidra.app.script.GhidraScript;
import ghidra.app.decompiler.DecompInterface;
import ghidra.app.decompiler.DecompileResults;
import ghidra.program.model.address.Address;
import ghidra.program.model.listing.Function;
import ghidra.program.model.listing.Instruction;
import ghidra.program.model.listing.InstructionIterator;

import java.util.TreeMap;

public class DumpGateDecoder extends GhidraScript {

    private static final long IMAGE_START = 0x00400000L;
    private static final long IMAGE_END = 0x00471000L;

    @Override
    public void run() throws Exception {
        long tableBase = 0x427619L;
        println("PROGRAM " + currentProgram.getName() + " imagebase " + currentProgram.getImageBase());

        Address decoder = toAddr(0x427580L);
        Function fn = getFunctionAt(decoder);
        println("DECODER " + (fn == null ? "no function" : fn.getName() + " size " + fn.getBody().getNumAddresses()));
        if (fn != null) {
            InstructionIterator it = currentProgram.getListing().getInstructions(fn.getBody(), true);
            while (it.hasNext()) {
                Instruction ins = it.next();
                println("  " + ins.getAddress() + "  " + ins.toString());
            }
        }

        // The jump table: JMP [EAX*2 + 0x427619] with EAX = the byte LODSB read.
        TreeMap<Long, String> targets = new TreeMap<>();
        int inImage = 0;
        for (int b = 0; b < 256; b++) {
            Address slot = toAddr(tableBase + 2L * b);
            long value = currentProgram.getMemory().getInt(slot) & 0xffffffffL;
            if (value >= IMAGE_START && value < IMAGE_END) {
                inImage++;
                targets.put(value, "b=" + String.format("%02x", b));
                println(String.format("ENTRY b=%02x slot=%s value=%08x in-image=yes", b, slot, value));
            } else {
                println(String.format("ENTRY b=%02x slot=%s value=%08x in-image=no", b, slot, value));
            }
        }
        println("IN_IMAGE_ENTRIES " + inImage + " of 256");

        DecompInterface ifc = new DecompInterface();
        ifc.openProgram(currentProgram);
        for (long value : targets.keySet()) {
            Address target = toAddr(value);
            // -noanalysis leaves these undisassembled; disassemble just the slot targets.
            if (currentProgram.getListing().getInstructionAt(target) == null) {
                disassemble(target);
            }
            Instruction first = currentProgram.getListing().getInstructionAt(target);
            println("TARGET " + target + " " + targets.get(value) + " first=" + (first == null ? "none" : first.toString()));
            Function f = getFunctionContaining(target);
            if (f == null) {
                f = createFunction(target, "gate_handler_" + String.format("%08x", value));
            }
            if (f != null) {
                DecompileResults r = ifc.decompileFunction(f, 60, monitor);
                if (r.decompileCompleted() && r.getDecompiledFunction() != null) {
                    println(r.getDecompiledFunction().getC());
                } else {
                    println("DECOMPILE_FAILED " + f.getName());
                }
            }
        }
    }
}
