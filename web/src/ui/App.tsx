import Box from "@mui/material/Box";
import Container from "@mui/material/Container";
import CssBaseline from "@mui/material/CssBaseline";
import Typography from "@mui/material/Typography";
import { UrlsView } from "./UrlsView.tsx";

/**
 * The shell: a heading, the view, and nothing else.
 *
 * There is deliberately no sign-in here. Whether this page is reached
 * through something that authenticates is the PLATFORM's decision, made at
 * the route in front of it, and a front end that drew its own badge would
 * be claiming to know an answer it is not told. An install with no gateway
 * in front serves the same page to whoever asks, which is the correct
 * behaviour for an example and a deliberate one for a deployment.
 */
export function App(): React.JSX.Element {
  return (
    <>
      {/* Normalises the browser's defaults, so the page looks the same
          whatever is rendering it. */}
      <CssBaseline />
      <Container maxWidth="md" sx={{ py: 4 }}>
        <Box sx={{ display: "flex", alignItems: "flex-start", mb: 3 }}>
          <Typography variant="h4" component="h1" sx={{ flex: 1, fontWeight: 600 }}>
            🔗 URL Shortener
          </Typography>
        </Box>
        <UrlsView />
      </Container>
    </>
  );
}
