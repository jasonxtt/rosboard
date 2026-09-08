export async function load(url, context, nextLoad) {
  if (/\.(css|svg|png|woff2)$/.test(url)) return { format: 'module', source: `export default ${JSON.stringify(url)}`, shortCircuit: true }
  return nextLoad(url, context)
}
