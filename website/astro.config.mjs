import * as path from 'path';
import { defineConfig } from 'astro/config';
import mdx from '@astrojs/mdx';
import mermaid from "astro-mermaid";


// https://astro.build/config
export default defineConfig({
  integrations: [
    mdx(),
    mermaid({
      theme: "neutral",
      autoTheme: true,
    }),
    {
      name: "watcher-extension",
      hooks: {
        "astro:server:setup": ({ server }) => {
          // Watch for changes to the extension json files to re-render the extension pages automatically in dev mode
          // There is apparently also "astro:config:setup" which is supposed to contain a method `addWatchFile` which does exactly this in one line
          // alas I couldn't get it to as the options passed into the hook was `undefined` and therefore didn't contain the method.
          const publicPath = path.resolve("public/");

          // Only in dev mode
          if (server.config.mode !== "development") {
            return;
          }

          server.watcher.add(publicPath);
          server.watcher
            .on("add", (path) => {
              if (
                path.endsWith("extensions.json") ||
                path.endsWith("extension-sets.json")
              ) {
                console.log(`File ${path} has been added; restarting`);
                server.restart();
              }
            })
            .on("change", (path) => {
              if (
                path.endsWith("extensions.json") ||
                path.endsWith("extension-sets.json")
              ) {
                console.log(`File ${path} has been changed; restarting`);
                server.restart();
              }
            });
        },
      },
    },
  ],
});
