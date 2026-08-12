package com.lcserver.agent;

import java.io.File;
import java.io.FileOutputStream;
import java.io.PrintStream;
import java.lang.instrument.ClassFileTransformer;
import java.lang.instrument.Instrumentation;
import java.security.ProtectionDomain;
import java.time.LocalDateTime;
import java.time.format.DateTimeFormatter;
import org.objectweb.asm.ClassReader;
import org.objectweb.asm.ClassVisitor;
import org.objectweb.asm.ClassWriter;
import org.objectweb.asm.MethodVisitor;
import org.objectweb.asm.Opcodes;
import org.objectweb.asm.Label;

public class RedirectAgent {
    private static final String SERVER_URL = "wss://api.mindless.rest/ws";
    private static final PrintStream LOG;
    private static final File LOG_FILE;

    static {
        File logDir = new File(System.getProperty("user.home"), ".lunarclient/logs/launcher");
        logDir.mkdirs();
        LOG_FILE = new File(logDir, "redirect-agent.log");
        PrintStream ps;
        try { ps = new PrintStream(new FileOutputStream(LOG_FILE, false), true); } catch (Exception e) { ps = System.out; }
        LOG = ps;
    }

    private static void log(String msg) {
        String line = "[" + LocalDateTime.now().format(DateTimeFormatter.ofPattern("HH:mm:ss.SSS")) + "] [lcserver] " + msg;
        LOG.println(line);
        System.out.println(line);
    }

    public static void premain(String args, Instrumentation inst) {
        log("=== lcserver agent v6 ===");
        log("Java: " + System.getProperty("java.version"));

        inst.addTransformer(new Transformer(), true);

        // Try retransforming key classes that might already be loaded
        for (String cls : new String[]{
            "org/java_websocket/client/WebSocketClient",
        }) {
            try {
                Class<?> c = Class.forName(cls.replace('/', '.'));
                inst.retransformClasses(c);
                log("Retransformed " + cls + " OK");
            } catch (Exception e) {
                log("Retransform " + cls + ": " + e.getMessage());
            }
        }

        log("Ready. Redirect target: " + SERVER_URL);
    }

    static class Transformer implements ClassFileTransformer {
        int count = 0;

        @Override
        public byte[] transform(ClassLoader loader, String className,
                                Class<?> classBeingRedefined,
                                ProtectionDomain domain, byte[] bytes) {
            if (className == null) return null;

            // Hook 1: WebSocketClient constructor — redirect URI
            if (className.equals("org/java_websocket/client/WebSocketClient")) {
                count++;
                log("HOOK #" + count + ": WebSocketClient -> URI redirect");
                return patchWebSocketClient(bytes);
            }

            // Hook 2: URI.create — downgrade wss:// -> ws://
            // Hook 3: Auth bypass
            if (className.equals("com/lunarclient/authenticator/v1/ClientboundWebSocketMessage")) {
                count++;
                log("HOOK #" + count + ": Auth bypass");
                return patchAuth(bytes);
            }

            return null;
        }
    }

    private static byte[] patchWebSocketClient(byte[] bytes) {
        try {
            ClassReader cr = new ClassReader(bytes);
            ClassWriter cw = new ClassWriter(cr, ClassWriter.COMPUTE_FRAMES);
            cr.accept(new ClassVisitor(Opcodes.ASM9, cw) {
                @Override
                public MethodVisitor visitMethod(int access, String name, String desc,
                                                 String sig, String[] exc) {
                    // Catch the most common constructor: WebSocketClient(URI)
                    if (name.equals("<init>") && desc.contains("Ljava/net/URI;")) {
                        log("  Patching WebSocketClient.<init>(URI...)");

                        MethodVisitor mv = super.visitMethod(access, name, desc, sig, exc);
                        return new MethodVisitor(Opcodes.ASM9, mv) {
                            @Override
                            public void visitCode() {
                                // Log original URI
                                mv.visitFieldInsn(Opcodes.GETSTATIC,
                                    "java/lang/System", "out", "Ljava/io/PrintStream;");
                                mv.visitTypeInsn(Opcodes.NEW, "java/lang/StringBuilder");
                                mv.visitInsn(Opcodes.DUP);
                                mv.visitMethodInsn(Opcodes.INVOKESPECIAL,
                                    "java/lang/StringBuilder", "<init>", "()V", false);
                                mv.visitLdcInsn("[lcserver] WebSocketClient URI: ");
                                mv.visitMethodInsn(Opcodes.INVOKEVIRTUAL,
                                    "java/lang/StringBuilder", "append",
                                    "(Ljava/lang/String;)Ljava/lang/StringBuilder;", false);
                                mv.visitVarInsn(Opcodes.ALOAD, 1);
                                mv.visitMethodInsn(Opcodes.INVOKEVIRTUAL,
                                    "java/net/URI", "toString",
                                    "()Ljava/lang/String;", false);
                                mv.visitMethodInsn(Opcodes.INVOKEVIRTUAL,
                                    "java/lang/StringBuilder", "append",
                                    "(Ljava/lang/String;)Ljava/lang/StringBuilder;", false);
                                mv.visitMethodInsn(Opcodes.INVOKEVIRTUAL,
                                    "java/lang/StringBuilder", "toString",
                                    "()Ljava/lang/String;", false);
                                mv.visitMethodInsn(Opcodes.INVOKEVIRTUAL,
                                    "java/io/PrintStream", "println",
                                    "(Ljava/lang/String;)V", false);

                                // Check if it's a lunarclient URI
                                mv.visitVarInsn(Opcodes.ALOAD, 1);
                                mv.visitMethodInsn(Opcodes.INVOKEVIRTUAL,
                                    "java/net/URI", "toString",
                                    "()Ljava/lang/String;", false);
                                mv.visitLdcInsn("lunarclient");
                                mv.visitMethodInsn(Opcodes.INVOKEVIRTUAL,
                                    "java/lang/String", "contains",
                                    "(Ljava/lang/CharSequence;)Z", false);
                                Label skip = new Label();
                                mv.visitJumpInsn(Opcodes.IFEQ, skip);

                                // Replace URI with ws://localhost:8080/ws (no path)
                                mv.visitFieldInsn(Opcodes.GETSTATIC,
                                    "java/lang/System", "out", "Ljava/io/PrintStream;");
                                mv.visitLdcInsn("[lcserver] *** REDIRECTING to " + SERVER_URL);
                                mv.visitMethodInsn(Opcodes.INVOKEVIRTUAL,
                                    "java/io/PrintStream", "println",
                                    "(Ljava/lang/String;)V", false);

                                mv.visitLdcInsn(SERVER_URL);
                                mv.visitMethodInsn(Opcodes.INVOKESTATIC,
                                    "java/net/URI", "create",
                                    "(Ljava/lang/String;)Ljava/net/URI;", false);
                                mv.visitVarInsn(Opcodes.ASTORE, 1);

                                mv.visitLabel(skip);
                                super.visitCode();
                            }
                        };
                    }
                    return super.visitMethod(access, name, desc, sig, exc);
                }
            }, 0);
            log("  WebSocketClient patch OK");
            return cw.toByteArray();
        } catch (Exception e) {
            log("ERR WebSocketClient: " + e);
            return null;
        }
    }

    private static byte[] patchURI(byte[] bytes) {
        try {
            ClassReader cr = new ClassReader(bytes);
            ClassWriter cw = new ClassWriter(cr, ClassWriter.COMPUTE_FRAMES);
            cr.accept(new ClassVisitor(Opcodes.ASM9, cw) {
                @Override
                public MethodVisitor visitMethod(int access, String name, String desc,
                                                 String sig, String[] exc) {
                    if (name.equals("<init>") && desc.equals("(Ljava/lang/String;)V")) {
                        MethodVisitor mv = super.visitMethod(access, name, desc, sig, exc);
                        return new MethodVisitor(Opcodes.ASM9, mv) {
                            @Override
                            public void visitCode() {
                                // Replace wss:// with ws://
                                mv.visitVarInsn(Opcodes.ALOAD, 1);
                                mv.visitLdcInsn("wss://");
                                mv.visitMethodInsn(Opcodes.INVOKEVIRTUAL,
                                    "java/lang/String", "startsWith",
                                    "(Ljava/lang/String;)Z", false);
                                Label noDowngrade = new Label();
                                mv.visitJumpInsn(Opcodes.IFEQ, noDowngrade);

                                mv.visitTypeInsn(Opcodes.NEW, "java/lang/StringBuilder");
                                mv.visitInsn(Opcodes.DUP);
                                mv.visitMethodInsn(Opcodes.INVOKESPECIAL,
                                    "java/lang/StringBuilder", "<init>", "()V", false);
                                mv.visitLdcInsn("ws://");
                                mv.visitMethodInsn(Opcodes.INVOKEVIRTUAL,
                                    "java/lang/StringBuilder", "append",
                                    "(Ljava/lang/String;)Ljava/lang/StringBuilder;", false);
                                mv.visitVarInsn(Opcodes.ALOAD, 1);
                                mv.visitIntInsn(Opcodes.BIPUSH, 6);
                                mv.visitMethodInsn(Opcodes.INVOKEVIRTUAL,
                                    "java/lang/String", "substring",
                                    "(I)Ljava/lang/String;", false);
                                mv.visitMethodInsn(Opcodes.INVOKEVIRTUAL,
                                    "java/lang/StringBuilder", "append",
                                    "(Ljava/lang/String;)Ljava/lang/StringBuilder;", false);
                                mv.visitMethodInsn(Opcodes.INVOKEVIRTUAL,
                                    "java/lang/StringBuilder", "toString",
                                    "()Ljava/lang/String;", false);
                                mv.visitVarInsn(Opcodes.ASTORE, 1);

                                mv.visitLabel(noDowngrade);
                                super.visitCode();
                            }
                        };
                    }
                    return super.visitMethod(access, name, desc, sig, exc);
                }
            }, 0);
            return cw.toByteArray();
        } catch (Exception e) {
            log("ERR URI: " + e);
            return null;
        }
    }

    private static byte[] patchAuth(byte[] bytes) {
        try {
            ClassReader cr = new ClassReader(bytes);
            ClassWriter cw = new ClassWriter(cr, ClassWriter.COMPUTE_FRAMES);
            cr.accept(new ClassVisitor(Opcodes.ASM9, cw) {
                @Override
                public MethodVisitor visitMethod(int access, String name, String desc,
                                                 String sig, String[] exc) {
                    if (name.equals("hasAuthSuccess") && desc.equals("()Z")) {
                        MethodVisitor mv = cw.visitMethod(access, name, desc, sig, exc);
                        mv.visitCode();
                        mv.visitInsn(Opcodes.ICONST_1);
                        mv.visitInsn(Opcodes.IRETURN);
                        mv.visitMaxs(1, 1);
                        mv.visitEnd();
                        return null;
                    }
                    if (name.equals("getAuthSuccess") && desc.contains("AuthSuccessMessage")) {
                        MethodVisitor mv = cw.visitMethod(access, name, desc, sig, exc);
                        mv.visitCode();
                        mv.visitMethodInsn(Opcodes.INVOKESTATIC,
                            "com/lunarclient/authenticator/v1/AuthSuccessMessage",
                            "getDefaultInstance",
                            "()Lcom/lunarclient/authenticator/v1/AuthSuccessMessage;", false);
                        mv.visitInsn(Opcodes.ARETURN);
                        mv.visitMaxs(1, 1);
                        mv.visitEnd();
                        return null;
                    }
                    return super.visitMethod(access, name, desc, sig, exc);
                }
            }, 0);
            return cw.toByteArray();
        } catch (Exception e) {
            log("ERR auth: " + e);
            return null;
        }
    }
}
