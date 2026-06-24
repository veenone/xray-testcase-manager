// files.ts — shared file-reading utilities.

// fileToBase64 reads a File as base64-encoded binary data.
// Returns the base64 string and whether the file is an XLSX (by extension).
export function fileToBase64(file: File): Promise<{ b64: string; xlsx: boolean }> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(new Error("could not read file"));
    reader.onload = () => {
      const bytes = new Uint8Array(reader.result as ArrayBuffer);
      let binary = "";
      const chunk = 0x8000;
      for (let i = 0; i < bytes.length; i += chunk) {
        binary += String.fromCharCode(...bytes.subarray(i, i + chunk));
      }
      resolve({ b64: btoa(binary), xlsx: file.name.toLowerCase().endsWith(".xlsx") });
    };
    reader.readAsArrayBuffer(file);
  });
}
