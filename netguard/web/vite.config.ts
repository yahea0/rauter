import { defineConfig } from "vite";

// أثناء التطوير: npm run dev يفتح على 5173 ويحيل الطلبات إلى وكيل Go على 8080
export default defineConfig({
  // إعداد PostCSS فارغ مضمّن — يمنع Vite من البحث عن ملفات إعداد في المجلدات الأب
  css: {
    postcss: {},
  },
  server: {
    proxy: {
      "/api": {
        target: "http://127.0.0.1:8080",
        changeOrigin: true,
      },
    },
  },
});
