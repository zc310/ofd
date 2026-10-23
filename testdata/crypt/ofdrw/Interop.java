import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.security.Security;
import java.security.cert.Certificate;
import java.security.cert.CertificateFactory;
import java.util.Arrays;
import java.util.HashSet;
import java.util.Set;
import org.bouncycastle.crypto.params.ECPrivateKeyParameters;
import org.bouncycastle.crypto.util.PrivateKeyFactory;
import org.bouncycastle.jce.provider.BouncyCastleProvider;
import org.ofdrw.crypto.OFDDecryptor;
import org.ofdrw.crypto.OFDEncryptor;
import org.ofdrw.crypto.decryptor.UserCertDecryptor;
import org.ofdrw.crypto.decryptor.UserPasswordDecryptor;
import org.ofdrw.crypto.enryptor.UserCertEncryptor;
import org.ofdrw.crypto.enryptor.UserPasswordEncryptor;
import org.ofdrw.gv.GlobalVar;

// Test-only adapter. The production classes are the unmodified 2.4.0 artifacts.
public final class Interop {
    public static void main(String[] args) throws Exception {
        if (!"2.4.0".equals(GlobalVar.Version)) {
            throw new IllegalStateException("Expected ofdrw 2.4.0, got " + GlobalVar.Version);
        }
        Security.addProvider(new BouncyCastleProvider());
        String mode = args[0];
        Path input = Paths.get(args[1]);
        Path output = Paths.get(args[2]);
        String user = args[3];
        Path credential = Paths.get(args[4]);
        if (mode.startsWith("encrypt-")) {
            Set<String> selected = new HashSet<>(Arrays.asList(args).subList(5, args.length));
            try (OFDEncryptor encryptor = new OFDEncryptor(input, output)) {
                if (!selected.isEmpty()) {
                    encryptor.setContainerFileFilter((path, absolute) -> selected.contains(path));
                }
                if (mode.equals("encrypt-password")) {
                    String password = new String(Files.readAllBytes(credential), StandardCharsets.UTF_8);
                    encryptor.addUser(new UserPasswordEncryptor(user, password));
                } else {
                    Certificate cert;
                    try (java.io.InputStream in = Files.newInputStream(credential)) {
                        cert = CertificateFactory.getInstance("X.509", "BC").generateCertificate(in);
                    }
                    encryptor.addUser(new UserCertEncryptor(user, cert));
                }
                encryptor.encrypt();
            }
        } else {
            try (OFDDecryptor decryptor = new OFDDecryptor(input, output)) {
                if (mode.equals("decrypt-password")) {
                    String password = new String(Files.readAllBytes(credential), StandardCharsets.UTF_8);
                    decryptor.addUser(new UserPasswordDecryptor(user, password));
                } else {
                    ECPrivateKeyParameters key = (ECPrivateKeyParameters) PrivateKeyFactory.createKey(Files.readAllBytes(credential));
                    decryptor.addUser(new UserCertDecryptor(user, key));
                }
                decryptor.decrypt();
            }
        }
        System.out.println("ofdrw " + GlobalVar.Version + " " + mode + " complete");
    }
}
