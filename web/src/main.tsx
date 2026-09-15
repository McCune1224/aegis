import { render } from "@solidjs/web";
import "./app.css";
import App from "./App";

const root = document.getElementById("root");
if (!root) {
  throw new Error("aegis: #root is missing from the document");
}

render(() => <App />, root);
