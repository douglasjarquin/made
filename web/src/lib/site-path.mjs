const baseUrl = `${import.meta.env.BASE_URL.replace(/\/+$/, "")}/`;

export function sitePath(relativePath = "") {
  const cleanPath = relativePath.replace(/^\/+|\/+$/g, "");
  return cleanPath.length === 0 ? baseUrl : `${baseUrl}${cleanPath}/`;
}
