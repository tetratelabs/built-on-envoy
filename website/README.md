# Built On Envoy Website

The official website for the Built On Envoy project by Tetrate.

## Development

### Prerequisites

- Node.js 18+ and npm

### Getting Started

Install dependencies:

```bash
npm install
```

Start the development server:

```bash
npm run dev
```

The site will be available at `http://localhost:4321`

### Building for Production

Build the site:

```bash
npm run build
```

Preview the production build:

```bash
npm run preview
```

## Deployment

This site is configured for deployment on Netlify. Simply connect your repository to Netlify and it will automatically deploy on every push to the main branch.

### Netlify Configuration

The `../netlify.toml` file is already configured with:
- Build command: `npm run build`
- Publish directory: `dist`

### Manual Deployment

You can also deploy manually:

```bash
npm run build
netlify deploy --prod --dir=dist
```
