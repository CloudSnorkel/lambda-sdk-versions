package sdkver;

import com.amazonaws.services.lambda.runtime.Context;
import com.amazonaws.services.lambda.runtime.RequestHandler;
import java.util.Collections;
import java.util.Map;

public class Handler implements RequestHandler<Object, Map<String, String>> {
    @Override
    public Map<String, String> handleRequest(Object input, Context context) {
        String version = null;
        Package[] packages = Package.getPackages();
        for (Package pkg : packages) {
            if ("software.amazon.awssdk".equals(pkg.getName())) {
                version = pkg.getImplementationVersion();
                break;
            }
        }
        if (version == null) {
            throw new RuntimeException("AWS SDK version not found in the classpath.");
        }
        return Collections.singletonMap("version", version);
    }
}