#!/bin/bash

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

LANG_PREF=$(cat ~/.s2ui_lang 2>/dev/null || echo "en")
# The menu wrote "zh" before it settled on the frontend's "zhcn", which is also
# what install.sh writes. Keep reading what older installs left behind.
[[ $LANG_PREF == "zh" ]] && LANG_PREF="zhcn"

t() {
    local key=$1
    if [[ $LANG_PREF == "zhcn" ]]; then
        case $key in
            menu_title)        echo "2S-UI 管理脚本" ;;
            opt_exit)          echo "退出" ;;
            opt_install)       echo "安装" ;;
            opt_update)        echo "更新" ;;
            opt_custom_ver)    echo "自定义版本" ;;
            opt_uninstall)     echo "卸载" ;;
            opt_reset_admin)   echo "重置管理员凭据为默认值" ;;
            opt_set_admin)     echo "设置管理员凭据" ;;
            opt_view_admin)    echo "查看管理员凭据" ;;
            opt_reset_panel)   echo "重置面板设置" ;;
            opt_set_panel)     echo "设置面板参数" ;;
            opt_view_panel)    echo "查看面板设置" ;;
            opt_start)         echo "2S-UI 启动" ;;
            opt_stop)          echo "2S-UI 停止" ;;
            opt_restart)       echo "2S-UI 重启" ;;
            opt_status)        echo "2S-UI 查看状态" ;;
            opt_log)           echo "2S-UI 查看日志" ;;
            opt_enable)        echo "2S-UI 开启开机自启" ;;
            opt_disable)       echo "2S-UI 关闭开机自启" ;;
            opt_bbr)           echo "开启或关闭 BBR" ;;
            opt_ssl)           echo "SSL 证书管理" ;;
            opt_cf_ssl)        echo "Cloudflare SSL 证书" ;;
            opt_lang)          echo "Language / 切换语言" ;;
            prompt_select)     echo "请输入选项 [0-21]: " ;;
            state_running)     echo "运行中" ;;
            state_stopped)     echo "未运行" ;;
            state_not_inst)    echo "未安装" ;;
            autostart_yes)     echo "是" ;;
            autostart_no)      echo "否" ;;
            autostart_label)   echo "开机自启" ;;
            err_not_root)      echo "错误：请以 root 身份运行此脚本！" ;;
            err_install_first) echo "请先安装面板" ;;
            err_already_inst)  echo "面板已安装，请勿重复安装" ;;
            err_invalid_num)   echo "请输入正确的选项 [0-21]" ;;
            msg_press_enter)   echo "按回车返回主菜单：" ;;
            msg_restart_svc)   echo "重启服务" ;;
            msg_cancelled)     echo "已取消" ;;
            confirm_update)    echo "此操作将强制重装最新版，数据不会丢失，是否继续？" ;;
            confirm_uninstall) echo "确定要卸载面板吗？" ;;
            confirm_reset_admin) echo "确定要将管理员凭据重置为默认值吗？" ;;
            confirm_reset_panel) echo "确定要将设置重置为默认值吗？" ;;
            warn_reset_admin)  echo "不建议将管理员凭据重置为默认值！" ;;
            warn_set_admin)    echo "不建议将管理员凭据设置为简单文本。" ;;
            prompt_username)   echo "请输入用户名：" ;;
            prompt_password)   echo "请输入密码：" ;;
            prompt_panel_port) echo "输入面板端口（留空保留现有/默认值）：" ;;
            prompt_panel_path) echo "输入面板路径（留空保留现有/默认值）：" ;;
            prompt_sub_port)   echo "输入订阅端口（留空保留现有/默认值）：" ;;
            prompt_sub_path)   echo "输入订阅路径（留空保留现有/默认值）：" ;;
            msg_initializing)  echo "初始化中，请稍候..." ;;
            msg_uninstalled)   echo "卸载成功，如需删除脚本，退出后执行 rm /usr/local/s-ui -f 命令。" ;;
            msg_panel_uri)     echo "面板与订阅地址：" ;;
            msg_lang_select)   echo "选择语言 / Select language:" ;;
            msg_lang_saved)    echo "语言已切换为简体中文。" ;;
            msg_panel_version) echo "输入面板版本（如 0.0.1）：" ;;
            msg_downloading)   echo "正在下载并安装版本" ;;
            *) echo "$key" ;;
        esac
    elif [[ $LANG_PREF == "zhtw" ]]; then
        case $key in
            menu_title)        echo "2S-UI 管理腳本" ;;
            opt_exit)          echo "退出" ;;
            opt_install)       echo "安裝" ;;
            opt_update)        echo "更新" ;;
            opt_custom_ver)    echo "自訂版本" ;;
            opt_uninstall)     echo "解除安裝" ;;
            opt_reset_admin)   echo "重設管理員憑證為預設值" ;;
            opt_set_admin)     echo "設定管理員憑證" ;;
            opt_view_admin)    echo "檢視管理員憑證" ;;
            opt_reset_panel)   echo "重設面板設定" ;;
            opt_set_panel)     echo "設定面板參數" ;;
            opt_view_panel)    echo "檢視面板設定" ;;
            opt_start)         echo "2S-UI 啟動" ;;
            opt_stop)          echo "2S-UI 停止" ;;
            opt_restart)       echo "2S-UI 重啟" ;;
            opt_status)        echo "2S-UI 檢視狀態" ;;
            opt_log)           echo "2S-UI 檢視日誌" ;;
            opt_enable)        echo "2S-UI 開啟開機自啟" ;;
            opt_disable)       echo "2S-UI 關閉開機自啟" ;;
            opt_bbr)           echo "開啟或關閉 BBR" ;;
            opt_ssl)           echo "SSL 憑證管理" ;;
            opt_cf_ssl)        echo "Cloudflare SSL 憑證" ;;
            opt_lang)          echo "Language / 切換語言" ;;
            prompt_select)     echo "請輸入選項 [0-21]: " ;;
            state_running)     echo "運行中" ;;
            state_stopped)     echo "未運行" ;;
            state_not_inst)    echo "未安裝" ;;
            autostart_yes)     echo "是" ;;
            autostart_no)      echo "否" ;;
            autostart_label)   echo "開機自啟" ;;
            err_not_root)      echo "錯誤：請以 root 身分執行此腳本！" ;;
            err_install_first) echo "請先安裝面板" ;;
            err_already_inst)  echo "面板已安裝，請勿重複安裝" ;;
            err_invalid_num)   echo "請輸入正確的選項 [0-21]" ;;
            msg_press_enter)   echo "按 Enter 返回主選單：" ;;
            msg_restart_svc)   echo "重啟服務" ;;
            msg_cancelled)     echo "已取消" ;;
            confirm_update)    echo "此操作將強制重裝最新版，資料不會遺失，是否繼續？" ;;
            confirm_uninstall) echo "確定要解除安裝面板嗎？" ;;
            confirm_reset_admin) echo "確定要將管理員憑證重設為預設值嗎？" ;;
            confirm_reset_panel) echo "確定要將設定重設為預設值嗎？" ;;
            warn_reset_admin)  echo "不建議將管理員憑證重設為預設值！" ;;
            warn_set_admin)    echo "不建議將管理員憑證設定為簡單文字。" ;;
            prompt_username)   echo "請輸入使用者名稱：" ;;
            prompt_password)   echo "請輸入密碼：" ;;
            prompt_panel_port) echo "輸入面板連接埠（留空保留現有/預設值）：" ;;
            prompt_panel_path) echo "輸入面板路徑（留空保留現有/預設值）：" ;;
            prompt_sub_port)   echo "輸入訂閱連接埠（留空保留現有/預設值）：" ;;
            prompt_sub_path)   echo "輸入訂閱路徑（留空保留現有/預設值）：" ;;
            msg_initializing)  echo "初始化中，請稍候..." ;;
            msg_uninstalled)   echo "解除安裝成功，如需刪除腳本，離開後執行 rm /usr/local/s-ui -f 指令。" ;;
            msg_panel_uri)     echo "面板與訂閱位址：" ;;
            msg_lang_select)   echo "選擇語言 / Select language:" ;;
            msg_lang_saved)    echo "語言已切換為繁體中文。" ;;
            msg_panel_version) echo "輸入面板版本（如 0.0.1）：" ;;
            msg_downloading)   echo "正在下載並安裝版本" ;;
            *) echo "$key" ;;
        esac
    elif [[ $LANG_PREF == "ru" ]]; then
        case $key in
            menu_title)        echo "Скрипт управления 2S-UI" ;;
            opt_exit)          echo "Выход" ;;
            opt_install)       echo "Установить" ;;
            opt_update)        echo "Обновить" ;;
            opt_custom_ver)    echo "Выбор версии" ;;
            opt_uninstall)     echo "Удалить" ;;
            opt_reset_admin)   echo "Сбросить учётные данные администратора" ;;
            opt_set_admin)     echo "Задать учётные данные администратора" ;;
            opt_view_admin)    echo "Показать учётные данные администратора" ;;
            opt_reset_panel)   echo "Сбросить настройки панели" ;;
            opt_set_panel)     echo "Настроить параметры панели" ;;
            opt_view_panel)    echo "Показать настройки панели" ;;
            opt_start)         echo "Запустить 2S-UI" ;;
            opt_stop)          echo "Остановить 2S-UI" ;;
            opt_restart)       echo "Перезапустить 2S-UI" ;;
            opt_status)        echo "Статус 2S-UI" ;;
            opt_log)           echo "Логи 2S-UI" ;;
            opt_enable)        echo "Включить автозапуск 2S-UI" ;;
            opt_disable)       echo "Отключить автозапуск 2S-UI" ;;
            opt_bbr)           echo "Включить или отключить BBR" ;;
            opt_ssl)           echo "Управление SSL-сертификатами" ;;
            opt_cf_ssl)        echo "SSL-сертификат Cloudflare" ;;
            opt_lang)          echo "Language / Язык" ;;
            prompt_select)     echo "Введите номер пункта [0-21]: " ;;
            state_running)     echo "Работает" ;;
            state_stopped)     echo "Не запущена" ;;
            state_not_inst)    echo "Не установлена" ;;
            autostart_yes)     echo "Да" ;;
            autostart_no)      echo "Нет" ;;
            autostart_label)   echo "Автозапуск" ;;
            err_not_root)      echo "ОШИБКА: запустите скрипт от имени root!" ;;
            err_install_first) echo "Сначала установите панель" ;;
            err_already_inst)  echo "Панель уже установлена, не устанавливайте повторно" ;;
            err_invalid_num)   echo "Введите корректный номер [0-21]" ;;
            msg_press_enter)   echo "Нажмите Enter для возврата в главное меню: " ;;
            msg_restart_svc)   echo "Перезапустить службу" ;;
            msg_cancelled)     echo "Отменено" ;;
            confirm_update)    echo "Будет принудительно переустановлена последняя версия, данные не будут потеряны. Продолжить?" ;;
            confirm_uninstall) echo "Вы уверены, что хотите удалить панель?" ;;
            confirm_reset_admin) echo "Сбросить учётные данные администратора к значениям по умолчанию?" ;;
            confirm_reset_panel) echo "Сбросить настройки к значениям по умолчанию?" ;;
            warn_reset_admin)  echo "Не рекомендуется сбрасывать учётные данные администратора к значениям по умолчанию!" ;;
            warn_set_admin)    echo "Не рекомендуется использовать простой текст в качестве учётных данных администратора." ;;
            prompt_username)   echo "Введите имя пользователя:" ;;
            prompt_password)   echo "Введите пароль:" ;;
            prompt_panel_port) echo "Порт панели (пусто — оставить текущее/по умолчанию):" ;;
            prompt_panel_path) echo "Путь панели (пусто — оставить текущее/по умолчанию):" ;;
            prompt_sub_port)   echo "Порт подписки (пусто — оставить текущее/по умолчанию):" ;;
            prompt_sub_path)   echo "Путь подписки (пусто — оставить текущее/по умолчанию):" ;;
            msg_initializing)  echo "Инициализация, подождите..." ;;
            msg_uninstalled)   echo "Удаление выполнено. Чтобы удалить сам скрипт, после выхода выполните rm /usr/local/s-ui -f" ;;
            msg_panel_uri)     echo "Адреса панели и подписки:" ;;
            msg_lang_select)   echo "Выберите язык / Select language:" ;;
            msg_lang_saved)    echo "Язык переключён на русский." ;;
            msg_panel_version) echo "Введите версию панели (например 0.0.1):" ;;
            msg_downloading)   echo "Загрузка и установка версии" ;;
            *) echo "$key" ;;
        esac
    elif [[ $LANG_PREF == "fa" ]]; then
        case $key in
            menu_title)        echo "اسکریپت مدیریت 2S-UI" ;;
            opt_exit)          echo "خروج" ;;
            opt_install)       echo "نصب" ;;
            opt_update)        echo "به‌روزرسانی" ;;
            opt_custom_ver)    echo "نسخه دلخواه" ;;
            opt_uninstall)     echo "حذف نصب" ;;
            opt_reset_admin)   echo "بازنشانی اطلاعات ورود مدیر به پیش‌فرض" ;;
            opt_set_admin)     echo "تنظیم اطلاعات ورود مدیر" ;;
            opt_view_admin)    echo "نمایش اطلاعات ورود مدیر" ;;
            opt_reset_panel)   echo "بازنشانی تنظیمات پنل" ;;
            opt_set_panel)     echo "تنظیم پارامترهای پنل" ;;
            opt_view_panel)    echo "نمایش تنظیمات پنل" ;;
            opt_start)         echo "راه‌اندازی 2S-UI" ;;
            opt_stop)          echo "توقف 2S-UI" ;;
            opt_restart)       echo "راه‌اندازی مجدد 2S-UI" ;;
            opt_status)        echo "وضعیت 2S-UI" ;;
            opt_log)           echo "لاگ‌های 2S-UI" ;;
            opt_enable)        echo "فعال‌سازی اجرای خودکار 2S-UI" ;;
            opt_disable)       echo "غیرفعال‌سازی اجرای خودکار 2S-UI" ;;
            opt_bbr)           echo "فعال یا غیرفعال کردن BBR" ;;
            opt_ssl)           echo "مدیریت گواهی SSL" ;;
            opt_cf_ssl)        echo "گواهی SSL کلادفلر" ;;
            opt_lang)          echo "Language / زبان" ;;
            prompt_select)     echo "شماره گزینه را وارد کنید [0-21]: " ;;
            state_running)     echo "در حال اجرا" ;;
            state_stopped)     echo "متوقف" ;;
            state_not_inst)    echo "نصب نشده" ;;
            autostart_yes)     echo "بله" ;;
            autostart_no)      echo "خیر" ;;
            autostart_label)   echo "اجرای خودکار" ;;
            err_not_root)      echo "خطا: این اسکریپت باید با دسترسی root اجرا شود!" ;;
            err_install_first) echo "ابتدا پنل را نصب کنید" ;;
            err_already_inst)  echo "پنل قبلاً نصب شده است، دوباره نصب نکنید" ;;
            err_invalid_num)   echo "یک شماره معتبر وارد کنید [0-21]" ;;
            msg_press_enter)   echo "برای بازگشت به منوی اصلی Enter بزنید: " ;;
            msg_restart_svc)   echo "راه‌اندازی مجدد سرویس" ;;
            msg_cancelled)     echo "لغو شد" ;;
            confirm_update)    echo "آخرین نسخه به‌صورت اجباری دوباره نصب می‌شود و داده‌ها از بین نمی‌روند. ادامه می‌دهید؟" ;;
            confirm_uninstall) echo "آیا از حذف نصب پنل مطمئن هستید؟" ;;
            confirm_reset_admin) echo "اطلاعات ورود مدیر به پیش‌فرض بازنشانی شود؟" ;;
            confirm_reset_panel) echo "تنظیمات به پیش‌فرض بازنشانی شود؟" ;;
            warn_reset_admin)  echo "بازنشانی اطلاعات ورود مدیر به پیش‌فرض توصیه نمی‌شود!" ;;
            warn_set_admin)    echo "استفاده از متن ساده برای اطلاعات ورود مدیر توصیه نمی‌شود." ;;
            prompt_username)   echo "نام کاربری را وارد کنید:" ;;
            prompt_password)   echo "رمز عبور را وارد کنید:" ;;
            prompt_panel_port) echo "پورت پنل (برای حفظ مقدار فعلی/پیش‌فرض خالی بگذارید):" ;;
            prompt_panel_path) echo "مسیر پنل (برای حفظ مقدار فعلی/پیش‌فرض خالی بگذارید):" ;;
            prompt_sub_port)   echo "پورت اشتراک (برای حفظ مقدار فعلی/پیش‌فرض خالی بگذارید):" ;;
            prompt_sub_path)   echo "مسیر اشتراک (برای حفظ مقدار فعلی/پیش‌فرض خالی بگذارید):" ;;
            msg_initializing)  echo "در حال آماده‌سازی، لطفاً صبر کنید..." ;;
            msg_uninstalled)   echo "حذف نصب با موفقیت انجام شد. برای حذف این اسکریپت پس از خروج دستور rm /usr/local/s-ui -f را اجرا کنید." ;;
            msg_panel_uri)     echo "نشانی‌های پنل و اشتراک:" ;;
            msg_lang_select)   echo "انتخاب زبان / Select language:" ;;
            msg_lang_saved)    echo "زبان به فارسی تغییر کرد." ;;
            msg_panel_version) echo "نسخه پنل را وارد کنید (مانند 0.0.1):" ;;
            msg_downloading)   echo "در حال دانلود و نصب نسخه" ;;
            *) echo "$key" ;;
        esac
    elif [[ $LANG_PREF == "vi" ]]; then
        case $key in
            menu_title)        echo "Tập lệnh quản trị 2S-UI" ;;
            opt_exit)          echo "Thoát" ;;
            opt_install)       echo "Cài đặt" ;;
            opt_update)        echo "Cập nhật" ;;
            opt_custom_ver)    echo "Phiên bản tùy chọn" ;;
            opt_uninstall)     echo "Gỡ cài đặt" ;;
            opt_reset_admin)   echo "Đặt lại thông tin đăng nhập quản trị về mặc định" ;;
            opt_set_admin)     echo "Đặt thông tin đăng nhập quản trị" ;;
            opt_view_admin)    echo "Xem thông tin đăng nhập quản trị" ;;
            opt_reset_panel)   echo "Đặt lại cài đặt bảng điều khiển" ;;
            opt_set_panel)     echo "Thiết lập tham số bảng điều khiển" ;;
            opt_view_panel)    echo "Xem cài đặt bảng điều khiển" ;;
            opt_start)         echo "Khởi động 2S-UI" ;;
            opt_stop)          echo "Dừng 2S-UI" ;;
            opt_restart)       echo "Khởi động lại 2S-UI" ;;
            opt_status)        echo "Trạng thái 2S-UI" ;;
            opt_log)           echo "Nhật ký 2S-UI" ;;
            opt_enable)        echo "Bật tự khởi động 2S-UI" ;;
            opt_disable)       echo "Tắt tự khởi động 2S-UI" ;;
            opt_bbr)           echo "Bật hoặc tắt BBR" ;;
            opt_ssl)           echo "Quản lý chứng chỉ SSL" ;;
            opt_cf_ssl)        echo "Chứng chỉ SSL Cloudflare" ;;
            opt_lang)          echo "Language / Ngôn ngữ" ;;
            prompt_select)     echo "Nhập lựa chọn [0-21]: " ;;
            state_running)     echo "Đang chạy" ;;
            state_stopped)     echo "Không chạy" ;;
            state_not_inst)    echo "Chưa cài đặt" ;;
            autostart_yes)     echo "Có" ;;
            autostart_no)      echo "Không" ;;
            autostart_label)   echo "Tự khởi động" ;;
            err_not_root)      echo "LỖI: Bạn phải chạy tập lệnh này với quyền root!" ;;
            err_install_first) echo "Vui lòng cài đặt bảng điều khiển trước" ;;
            err_already_inst)  echo "Bảng điều khiển đã được cài đặt, vui lòng không cài lại" ;;
            err_invalid_num)   echo "Vui lòng nhập số hợp lệ [0-21]" ;;
            msg_press_enter)   echo "Nhấn Enter để quay lại menu chính: " ;;
            msg_restart_svc)   echo "Khởi động lại dịch vụ" ;;
            msg_cancelled)     echo "Đã hủy" ;;
            confirm_update)    echo "Thao tác này sẽ cài lại phiên bản mới nhất, dữ liệu sẽ không bị mất. Tiếp tục?" ;;
            confirm_uninstall) echo "Bạn có chắc muốn gỡ cài đặt bảng điều khiển?" ;;
            confirm_reset_admin) echo "Đặt lại thông tin đăng nhập quản trị về mặc định?" ;;
            confirm_reset_panel) echo "Đặt lại cài đặt về mặc định?" ;;
            warn_reset_admin)  echo "Không nên đặt lại thông tin đăng nhập quản trị về mặc định!" ;;
            warn_set_admin)    echo "Không nên dùng văn bản đơn giản làm thông tin đăng nhập quản trị." ;;
            prompt_username)   echo "Nhập tên người dùng:" ;;
            prompt_password)   echo "Nhập mật khẩu:" ;;
            prompt_panel_port) echo "Nhập cổng bảng điều khiển (để trống giữ giá trị hiện tại/mặc định):" ;;
            prompt_panel_path) echo "Nhập đường dẫn bảng điều khiển (để trống giữ giá trị hiện tại/mặc định):" ;;
            prompt_sub_port)   echo "Nhập cổng đăng ký (để trống giữ giá trị hiện tại/mặc định):" ;;
            prompt_sub_path)   echo "Nhập đường dẫn đăng ký (để trống giữ giá trị hiện tại/mặc định):" ;;
            msg_initializing)  echo "Đang khởi tạo, vui lòng đợi..." ;;
            msg_uninstalled)   echo "Gỡ cài đặt thành công. Nếu muốn xóa tập lệnh này, sau khi thoát hãy chạy lệnh rm /usr/local/s-ui -f" ;;
            msg_panel_uri)     echo "Địa chỉ bảng điều khiển và đăng ký:" ;;
            msg_lang_select)   echo "Chọn ngôn ngữ / Select language:" ;;
            msg_lang_saved)    echo "Đã chuyển ngôn ngữ sang tiếng Việt." ;;
            msg_panel_version) echo "Nhập phiên bản bảng điều khiển (ví dụ 0.0.1):" ;;
            msg_downloading)   echo "Đang tải xuống và cài đặt phiên bản" ;;
            *) echo "$key" ;;
        esac
    else
        case $key in
            menu_title)        echo "2S-UI Admin Management Script" ;;
            opt_exit)          echo "Exit" ;;
            opt_install)       echo "Install" ;;
            opt_update)        echo "Update" ;;
            opt_custom_ver)    echo "Custom Version" ;;
            opt_uninstall)     echo "Uninstall" ;;
            opt_reset_admin)   echo "Reset admin credentials to default" ;;
            opt_set_admin)     echo "Set admin credentials" ;;
            opt_view_admin)    echo "View admin credentials" ;;
            opt_reset_panel)   echo "Reset Panel Settings" ;;
            opt_set_panel)     echo "Set Panel settings" ;;
            opt_view_panel)    echo "View Panel Settings" ;;
            opt_start)         echo "2S-UI Start" ;;
            opt_stop)          echo "2S-UI Stop" ;;
            opt_restart)       echo "2S-UI Restart" ;;
            opt_status)        echo "2S-UI Check State" ;;
            opt_log)           echo "2S-UI Check Logs" ;;
            opt_enable)        echo "2S-UI Enable Autostart" ;;
            opt_disable)       echo "2S-UI Disable Autostart" ;;
            opt_bbr)           echo "Enable or Disable BBR" ;;
            opt_ssl)           echo "SSL Certificate Management" ;;
            opt_cf_ssl)        echo "Cloudflare SSL Certificate" ;;
            opt_lang)          echo "Language / 切换语言" ;;
            prompt_select)     echo "Please enter your selection [0-21]: " ;;
            state_running)     echo "Running" ;;
            state_stopped)     echo "Not Running" ;;
            state_not_inst)    echo "Not Installed" ;;
            autostart_yes)     echo "Yes" ;;
            autostart_no)      echo "No" ;;
            autostart_label)   echo "Start automatically" ;;
            err_not_root)      echo "ERROR: You must be root to run this script!" ;;
            err_install_first) echo "Please install the panel first" ;;
            err_already_inst)  echo "Panel is already installed, Please do not reinstall" ;;
            err_invalid_num)   echo "Please enter the correct number [0-21]" ;;
            msg_press_enter)   echo "Press enter to return to the main menu: " ;;
            msg_restart_svc)   echo "Restart the service" ;;
            msg_cancelled)     echo "Cancelled" ;;
            confirm_update)    echo "This function will forcefully reinstall the latest version, and the data will not be lost. Do you want to continue?" ;;
            confirm_uninstall) echo "Are you sure you want to uninstall the panel?" ;;
            confirm_reset_admin) echo "Are you sure you want to reset admin's credentials to default?" ;;
            confirm_reset_panel) echo "Are you sure you want to reset settings to default?" ;;
            warn_reset_admin)  echo "It is not recommended to set admin's credentials to default!" ;;
            warn_set_admin)    echo "It is not recommended to set admin's credentials to a complex text." ;;
            prompt_username)   echo "Please set up your username:" ;;
            prompt_password)   echo "Please set up your password:" ;;
            prompt_panel_port) echo "Enter the panel port (leave blank for existing/default value):" ;;
            prompt_panel_path) echo "Enter the panel path (leave blank for existing/default value):" ;;
            prompt_sub_port)   echo "Enter the subscription port (leave blank for existing/default value):" ;;
            prompt_sub_path)   echo "Enter the subscription path (leave blank for existing/default value):" ;;
            msg_initializing)  echo "Initializing, please wait..." ;;
            msg_uninstalled)   echo "Uninstalled Successfully, If you want to remove this script, then after exiting the script run" ;;
            msg_panel_uri)     echo "Panel and subscription addresses:" ;;
            msg_lang_select)   echo "Select language / 选择语言:" ;;
            msg_lang_saved)    echo "Language switched to English." ;;
            msg_panel_version) echo "Enter the panel version (like 0.0.1):" ;;
            msg_downloading)   echo "Downloading and installing panel version" ;;
            *) echo "$key" ;;
        esac
    fi
}

function LOGD() {
    echo -e "${yellow}[DEG] $* ${plain}"
}

function LOGE() {
    echo -e "${red}[ERR] $* ${plain}"
}

function LOGI() {
    echo -e "${green}[INF] $* ${plain}"
}

[[ $EUID -ne 0 ]] && LOGE "$(t err_not_root)\n" && exit 1

if [[ -f /etc/os-release ]]; then
    source /etc/os-release
    release=$ID
elif [[ -f /usr/lib/os-release ]]; then
    source /usr/lib/os-release
    release=$ID
else
    echo "Failed to check the system OS, please contact the author!" >&2
    exit 1
fi

echo "The OS release is: $release"

# Detect the init system: systemd everywhere, OpenRC on Alpine.
# Keep this block in sync with the identical one in install.sh — the two
# scripts are downloaded independently and cannot share a source file.
if [[ "$release" == "alpine" ]]; then
    init_system="openrc"
elif command -v systemctl >/dev/null 2>&1 && [[ -d /run/systemd/system ]]; then
    init_system="systemd"
elif command -v rc-service >/dev/null 2>&1; then
    init_system="openrc"
else
    init_system="systemd"
fi

# Service wrappers so every menu action works on both init systems.
svc_start() { if [[ "${init_system}" == "openrc" ]]; then rc-service $1 start; else systemctl start $1; fi; }
svc_stop() { if [[ "${init_system}" == "openrc" ]]; then rc-service $1 stop; else systemctl stop $1; fi; }
svc_restart() { if [[ "${init_system}" == "openrc" ]]; then rc-service $1 restart; else systemctl restart $1; fi; }
svc_status() { if [[ "${init_system}" == "openrc" ]]; then rc-service $1 status; else systemctl status $1 -l; fi; }
svc_enable() { if [[ "${init_system}" == "openrc" ]]; then rc-update add $1 default; else systemctl enable $1; fi; }
svc_disable() { if [[ "${init_system}" == "openrc" ]]; then rc-update del $1 default; else systemctl disable $1; fi; }
# OpenRC has no journal; the init script sends both streams to this log.
# Create the file if missing so tail does not exit with "No such file"
# before the service has ever written anything (unlike journalctl).
svc_log() {
    if [[ "${init_system}" == "openrc" ]]; then
        local log=/var/log/s-ui.log
        if [[ ! -f "$log" ]]; then
            touch "$log" 2>/dev/null || {
                LOGE "Log file $log does not exist and could not be created"
                return 1
            }
        fi
        tail -n 200 -f "$log"
    else
        journalctl -u $1.service -e --no-pager -f
    fi
}

confirm() {
    if [[ $# > 1 ]]; then
        echo && read -p "$1 [Default$2]: " temp
        if [[ x"${temp}" == x"" ]]; then
            temp=$2
        fi
    else
        read -p "$1 [y/n]: " temp
    fi
    if [[ x"${temp}" == x"y" || x"${temp}" == x"Y" ]]; then
        return 0
    else
        return 1
    fi
}

confirm_restart() {
    confirm "$(t msg_restart_svc) ${1}" "y"
    if [[ $? == 0 ]]; then
        restart
    else
        show_menu
    fi
}

before_show_menu() {
    echo && echo -n -e "${yellow}$(t msg_press_enter)${plain}" && read temp
    show_menu
}

install() {
    bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/main/install.sh)
    if [[ $? == 0 ]]; then
        if [[ $# == 0 ]]; then
            start
        else
            start 0
        fi
    fi
}

update() {
    confirm "$(t confirm_update)" "n"
    if [[ $? != 0 ]]; then
        LOGE "$(t msg_cancelled)"
        if [[ $# == 0 ]]; then
            before_show_menu
        fi
        return 0
    fi
    bash <(curl -Ls https://raw.githubusercontent.com/shenaba/2s-ui/main/install.sh)
    if [[ $? == 0 ]]; then
        LOGI "Update is complete, Panel has automatically restarted "
        exit 0
    fi
}

custom_version() {
    echo "$(t msg_panel_version)"
    read panel_version

    if [ -z "$panel_version" ]; then
        echo "Panel version cannot be empty. Exiting."
    exit 1
    fi

    download_link="https://raw.githubusercontent.com/shenaba/2s-ui/main/install.sh"

    install_command="bash <(curl -Ls $download_link) $panel_version"

    echo "$(t msg_downloading) $panel_version..."
    eval $install_command
}

uninstall() {
    confirm "$(t confirm_uninstall)" "n"
    if [[ $? != 0 ]]; then
        if [[ $# == 0 ]]; then
            show_menu
        fi
        return 0
    fi
    if [[ "${init_system}" == "openrc" ]]; then
        rc-service s-ui stop
        rc-update del s-ui default
        rm /etc/init.d/s-ui -f
    else
        systemctl stop s-ui
        systemctl disable s-ui
        rm /etc/systemd/system/s-ui.service -f
        systemctl daemon-reload
        systemctl reset-failed
    fi
    rm /etc/s-ui/ -rf
    rm /usr/local/s-ui/ -rf
    rm /usr/bin/s-ui -f

    echo ""
    echo -e "$(t msg_uninstalled) ${green}rm /usr/local/s-ui -f${plain} to delete it."
    echo ""

    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

reset_admin() {
    echo "$(t warn_reset_admin)"
    confirm "$(t confirm_reset_admin)" "n"
    if [[ $? == 0 ]]; then
        /usr/local/s-ui/sui admin -reset
    fi
    before_show_menu
}

set_admin() {
    echo "$(t warn_set_admin)"
    read -p "$(t prompt_username)" config_account
    read -p "$(t prompt_password)" config_password
    /usr/local/s-ui/sui admin -username ${config_account} -password ${config_password}
    before_show_menu
}

view_admin() {
    /usr/local/s-ui/sui admin -show
    before_show_menu
}

reset_setting() {
    confirm "$(t confirm_reset_panel)" "n"
    if [[ $? == 0 ]]; then
        /usr/local/s-ui/sui setting -reset
    fi
    before_show_menu
}

set_setting() {
    echo -e "$(t prompt_panel_port)"
    read config_port
    echo -e "$(t prompt_panel_path)"
    read config_path

    echo -e "$(t prompt_sub_port)"
    read config_subPort
    echo -e "$(t prompt_sub_path)"
    read config_subPath

    echo -e "${yellow}$(t msg_initializing)${plain}"
    params=""
    [ -z "$config_port" ] || params="$params -port $config_port"
    [ -z "$config_path" ] || params="$params -path $config_path"
    [ -z "$config_subPort" ] || params="$params -subPort $config_subPort"
    [ -z "$config_subPath" ] || params="$params -subPath $config_subPath"
    /usr/local/s-ui/sui setting ${params}
    before_show_menu
}

view_setting() {
    /usr/local/s-ui/sui setting -show
    view_uri
    before_show_menu
}

view_uri() {
    info=$(/usr/local/s-ui/sui uri)
    if [[ $? != 0 ]]; then
        LOGE "Get current uri error"
        before_show_menu
    fi
    LOGI "$(t msg_panel_uri)"
    echo -e "${green}${info}${plain}"
}

start() {
    check_status $1
    if [[ $? == 0 ]]; then
        echo ""
        LOGI -e "${1} is running, No need to start again, If you need to restart, please select restart"
    else
        svc_start $1
        sleep 2
        check_status $1
        if [[ $? == 0 ]]; then
            LOGI "${1} Started Successfully"
        else
            LOGE "Failed to start ${1}, Probably because it takes longer than two seconds to start, Please check the log information later"
        fi
    fi

    if [[ $# == 1 ]]; then
        before_show_menu
    fi
}

stop() {
    check_status $1
    if [[ $? == 1 ]]; then
        echo ""
        LOGI "${1} stopped, No need to stop again!"
    else
        svc_stop $1
        sleep 2
        # Must pass $1 — empty name makes OpenRC look up /etc/init.d/ and
        # always report failure even when the stop itself succeeded.
        check_status $1
        if [[ $? == 1 ]]; then
            LOGI "${1} stopped successfully"
        else
            LOGE "Failed to stop ${1}, Probably because the stop time exceeds two seconds, Please check the log information later"
        fi
    fi

    if [[ $# == 1 ]]; then
        before_show_menu
    fi
}

restart() {
    svc_restart $1
    sleep 2
    check_status $1
    if [[ $? == 0 ]]; then
        LOGI "${1} Restarted successfully"
    else
        LOGE "Failed to restart ${1}, Probably because it takes longer than two seconds to start, Please check the log information later"
    fi
    if [[ $# == 1 ]]; then
        before_show_menu
    fi
}

status() {
    svc_status s-ui
    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

enable() {
    svc_enable $1
    if [[ $? == 0 ]]; then
        LOGI "Set ${1} to boot automatically on startup successfully"
    else
        LOGE "Failed to set ${1} Autostart"
    fi

    if [[ $# == 1 ]]; then
        before_show_menu
    fi
}

disable() {
    svc_disable $1
    if [[ $? == 0 ]]; then
        LOGI "Autostart ${1} Cancelled successfully"
    else
        LOGE "Failed to cancel ${1} autostart"
    fi

    if [[ $# == 1 ]]; then
        before_show_menu
    fi
}

show_log() {
    svc_log $1
    if [[ $# == 1 ]]; then
        before_show_menu
    fi
}

update_shell() {
    wget -O /usr/bin/s-ui -N --no-check-certificate https://github.com/shenaba/2s-ui/raw/main/s-ui.sh
    if [[ $? != 0 ]]; then
        echo ""
        LOGE "Failed to download script, Please check whether the machine can connect Github"
        before_show_menu
    else
        chmod +x /usr/bin/s-ui
        LOGI "Upgrade script succeeded, Please rerun the script" && exit 0
    fi
}

check_status() {
    # Contract used by show_status/start/stop/restart:
    #   0 = running, 1 = stopped/not running, 2 = not installed.
    # Do not pass rc-service's raw exit codes through — a stopped service
    # returns 3 (LSB), which matches none of the callers' cases.
    if [[ "${init_system}" == "openrc" ]]; then
        [[ -f "/etc/init.d/$1" ]] || return 2
        if rc-service $1 status >/dev/null 2>&1; then
            return 0
        fi
        return 1
    fi
    if [[ ! -f "/etc/systemd/system/$1.service" ]]; then
        return 2
    fi
    temp=$(systemctl status "$1" | grep Active | awk '{print $3}' | cut -d "(" -f2 | cut -d ")" -f1)
    if [[ x"${temp}" == x"running" ]]; then
        return 0
    else
        return 1
    fi
}

check_enabled() {
    if [[ "${init_system}" == "openrc" ]]; then
        rc-update show default 2>/dev/null | awk '{print $1}' | grep -qx "$1"
        return $?
    fi
    temp=$(systemctl is-enabled $1)
    if [[ x"${temp}" == x"enabled" ]]; then
        return 0
    else
        return 1
    fi
}

check_uninstall() {
    check_status s-ui
    if [[ $? != 2 ]]; then
        echo ""
        LOGE "$(t err_already_inst)"
        if [[ $# == 0 ]]; then
            before_show_menu
        fi
        return 1
    else
        return 0
    fi
}

check_install() {
    check_status s-ui
    if [[ $? == 2 ]]; then
        echo ""
        LOGE "$(t err_install_first)"
        if [[ $# == 0 ]]; then
            before_show_menu
        fi
        return 1
    else
        return 0
    fi
}

show_status() {
    check_status $1
    case $? in
    0)
        echo -e "${1} state: ${green}$(t state_running)${plain}"
        show_enable_status $1
        ;;
    1)
        echo -e "${1} state: ${yellow}$(t state_stopped)${plain}"
        show_enable_status $1
        ;;
    2)
        echo -e "${1} state: ${red}$(t state_not_inst)${plain}"
        ;;
    esac
}

show_enable_status() {
    check_enabled $1
    if [[ $? == 0 ]]; then
        echo -e "$(t autostart_label) ${1}: ${green}$(t autostart_yes)${plain}"
    else
        echo -e "$(t autostart_label) ${1}: ${red}$(t autostart_no)${plain}"
    fi
}

check_s-ui_status() {
    count=$(ps -ef | grep "sui" | grep -v "grep" | wc -l)
    if [[ count -ne 0 ]]; then
        return 0
    else
        return 1
    fi
}

show_s-ui_status() {
    check_s-ui_status
    if [[ $? == 0 ]]; then
        echo -e "s-ui state: ${green}Running${plain}"
    else
        echo -e "s-ui state: ${red}Not Running${plain}"
    fi
}

bbr_menu() {
    echo -e "${green}\t1.${plain} Enable BBR"
    echo -e "${green}\t2.${plain} Disable BBR"
    echo -e "${green}\t0.${plain} Back to Main Menu"
    read -p "Choose an option: " choice
    case "$choice" in
    0)
        show_menu
        ;;
    1)
        enable_bbr
        ;;
    2)
        disable_bbr
        ;;
    *) echo "Invalid choice" ;;
    esac
}

disable_bbr() {
    if ! grep -q "net.core.default_qdisc=fq" /etc/sysctl.conf || ! grep -q "net.ipv4.tcp_congestion_control=bbr" /etc/sysctl.conf; then
        echo -e "${yellow}BBR is not currently enabled.${plain}"
        exit 0
    fi
    sed -i 's/net.core.default_qdisc=fq/net.core.default_qdisc=pfifo_fast/' /etc/sysctl.conf
    sed -i 's/net.ipv4.tcp_congestion_control=bbr/net.ipv4.tcp_congestion_control=cubic/' /etc/sysctl.conf
    sysctl -p
    if [[ $(sysctl net.ipv4.tcp_congestion_control | awk '{print $3}') == "cubic" ]]; then
        echo -e "${green}BBR has been replaced with CUBIC successfully.${plain}"
    else
        echo -e "${red}Failed to replace BBR with CUBIC. Please check your system configuration.${plain}"
    fi
}

enable_bbr() {
    if grep -q "net.core.default_qdisc=fq" /etc/sysctl.conf && grep -q "net.ipv4.tcp_congestion_control=bbr" /etc/sysctl.conf; then
        echo -e "${green}BBR is already enabled!${plain}"
        exit 0
    fi
    case "${release}" in
    ubuntu | debian | armbian)
        apt-get update && apt-get install -yqq --no-install-recommends ca-certificates
        ;;
    centos | almalinux | rocky | oracle)
        yum -y update && yum -y install ca-certificates
        ;;
    fedora)
        dnf -y update && dnf -y install ca-certificates
        ;;
    arch | manjaro | parch)
        pacman -Sy --noconfirm ca-certificates
        ;;
    alpine)
        apk update && apk add --no-cache ca-certificates
        ;;
    *)
        echo -e "${red}Unsupported operating system. Please check the script and install the necessary packages manually.${plain}\n"
        exit 1
        ;;
    esac
    echo "net.core.default_qdisc=fq" | tee -a /etc/sysctl.conf
    echo "net.ipv4.tcp_congestion_control=bbr" | tee -a /etc/sysctl.conf
    sysctl -p
    if [[ $(sysctl net.ipv4.tcp_congestion_control | awk '{print $3}') == "bbr" ]]; then
        echo -e "${green}BBR has been enabled successfully.${plain}"
    else
        echo -e "${red}Failed to enable BBR. Please check your system configuration.${plain}"
    fi
}

install_acme() {
    cd ~
    LOGI "install acme..."
    curl https://get.acme.sh | sh
    if [ $? -ne 0 ]; then
        LOGE "install acme failed"
        return 1
    else
        LOGI "install acme succeed"
    fi
    return 0
}

ssl_cert_issue_main() {
    echo -e "${green}\t1.${plain} Get SSL"
    echo -e "${green}\t2.${plain} Revoke"
    echo -e "${green}\t3.${plain} Force Renew"
    echo -e "${green}\t4.${plain} Self-signed Certificate"
    read -p "Choose an option: " choice
    case "$choice" in
        1) ssl_cert_issue ;;
        2) 
            local domain=""
            read -p "Please enter your domain name to revoke the certificate: " domain
            ~/.acme.sh/acme.sh --revoke -d ${domain}
            LOGI "Certificate revoked"
            ;;
        3)
            local domain=""
            read -p "Please enter your domain name to forcefully renew an SSL certificate: " domain
            ~/.acme.sh/acme.sh --renew -d ${domain} --force ;;
        4)
            generate_self_signed_cert
            ;;
        *) echo "Invalid choice" ;;
    esac
}

ssl_cert_issue() {
    if ! command -v ~/.acme.sh/acme.sh &>/dev/null; then
        echo "acme.sh could not be found. we will install it"
        install_acme
        if [ $? -ne 0 ]; then
            LOGE "install acme failed, please check logs"
            exit 1
        fi
    fi
    case "${release}" in
    ubuntu | debian | armbian)
        apt update && apt install socat -y
        ;;
    centos | almalinux | rocky | oracle)
        yum -y update && yum -y install socat
        ;;
    fedora)
        dnf -y update && dnf -y install socat
        ;;
    arch | manjaro | parch)
        pacman -Sy --noconfirm socat
        ;;
    alpine)
        apk update && apk add --no-cache socat
        ;;
    *)
        echo -e "${red}Unsupported operating system. Please check the script and install the necessary packages manually.${plain}\n"
        exit 1
        ;;
    esac
    if [ $? -ne 0 ]; then
        LOGE "install socat failed, please check logs"
        exit 1
    else
        LOGI "install socat succeed..."
    fi

    local domain=""
    read -p "Please enter your domain name:" domain
    LOGD "your domain is:${domain},check it..."
    local currentCert=$(~/.acme.sh/acme.sh --list | tail -1 | awk '{print $1}')

    if [ ${currentCert} == ${domain} ]; then
        local certInfo=$(~/.acme.sh/acme.sh --list)
        LOGE "system already has certs here,can not issue again,current certs details:"
        LOGI "$certInfo"
        exit 1
    else
        LOGI "your domain is ready for issuing cert now..."
    fi

    certPath="/root/cert/${domain}"
    if [ ! -d "$certPath" ]; then
        mkdir -p "$certPath"
    else
        rm -rf "$certPath"
        mkdir -p "$certPath"
    fi

    local WebPort=80
    read -p "please choose which port do you use,default will be 80 port:" WebPort
    if [[ ${WebPort} -gt 65535 || ${WebPort} -lt 1 ]]; then
        LOGE "your input ${WebPort} is invalid,will use default port"
    fi
    LOGI "will use port:${WebPort} to issue certs,please make sure this port is open..."
    ~/.acme.sh/acme.sh --set-default-ca --server letsencrypt
    ~/.acme.sh/acme.sh --issue -d ${domain} --standalone --httpport ${WebPort}
    if [ $? -ne 0 ]; then
        LOGE "issue certs failed,please check logs"
        rm -rf ~/.acme.sh/${domain}
        exit 1
    else
        LOGE "issue certs succeed,installing certs..."
    fi
    ~/.acme.sh/acme.sh --installcert -d ${domain} \
        --key-file /root/cert/${domain}/privkey.pem \
        --fullchain-file /root/cert/${domain}/fullchain.pem

    if [ $? -ne 0 ]; then
        LOGE "install certs failed,exit"
        rm -rf ~/.acme.sh/${domain}
        exit 1
    else
        LOGI "install certs succeed,enable auto renew..."
    fi

    ~/.acme.sh/acme.sh --upgrade --auto-upgrade
    if [ $? -ne 0 ]; then
        LOGE "auto renew failed, certs details:"
        ls -lah cert/*
        chmod 755 $certPath/*
        exit 1
    else
        LOGI "auto renew succeed, certs details:"
        ls -lah cert/*
        chmod 755 $certPath/*
    fi
}

ssl_cert_issue_CF() {
    echo -E ""
    LOGD "******Instructions for use******"
    echo "1) New certificate from Cloudflare"
    echo "2) Force renew existing Certificates"
    echo "3) Back to Menu"
    read -p "Enter your choice [1-3]: " choice

    certPath="/root/cert-CF"

    case $choice in
        1|2)
            force_flag=""
            if [ "$choice" -eq 2 ]; then
                force_flag="--force"
                echo "Forcing SSL certificate reissuance..."
            else
                echo "Starting SSL certificate issuance..."
            fi
            
            LOGD "******Instructions for use******"
            LOGI "This Acme script requires the following data:"
            LOGI "1.Cloudflare Registered e-mail"
            LOGI "2.Cloudflare Global API Key"
            LOGI "3.The domain name that has been resolved DNS to the current server by Cloudflare"
            LOGI "4.The script applies for a certificate. The default installation path is /root/cert "
            confirm "Confirmed?[y/n]" "y"
            if [ $? -eq 0 ]; then
                if ! command -v ~/.acme.sh/acme.sh &>/dev/null; then
                    echo "acme.sh could not be found. Installing..."
                    install_acme
                    if [ $? -ne 0 ]; then
                        LOGE "Install acme failed, please check logs"
                        exit 1
                    fi
                fi

                CF_Domain=""
                if [ ! -d "$certPath" ]; then
                    mkdir -p $certPath
                else
                    rm -rf $certPath
                    mkdir -p $certPath
                fi

                LOGD "Please set a domain name:"
                read -p "Input your domain here: " CF_Domain
                LOGD "Your domain name is set to: ${CF_Domain}"

                CF_GlobalKey=""
                CF_AccountEmail=""
                LOGD "Please set the API key:"
                read -p "Input your key here: " CF_GlobalKey
                LOGD "Your API key is: ${CF_GlobalKey}"

                LOGD "Please set up registered email:"
                read -p "Input your email here: " CF_AccountEmail
                LOGD "Your registered email address is: ${CF_AccountEmail}"

                ~/.acme.sh/acme.sh --set-default-ca --server letsencrypt
                if [ $? -ne 0 ]; then
                    LOGE "Default CA, Let's Encrypt failed, script exiting..."
                    exit 1
                fi

                export CF_Key="${CF_GlobalKey}"
                export CF_Email="${CF_AccountEmail}"

                ~/.acme.sh/acme.sh --issue --dns dns_cf -d ${CF_Domain} -d *.${CF_Domain} $force_flag --log
                if [ $? -ne 0 ]; then
                    LOGE "Certificate issuance failed, script exiting..."
                    exit 1
                else
                    LOGI "Certificate issued Successfully, Installing..."
                fi

                mkdir -p ${certPath}/${CF_Domain}
                if [ $? -ne 0 ]; then
                    LOGE "Failed to create directory: ${certPath}/${CF_Domain}"
                    exit 1
                fi

                ~/.acme.sh/acme.sh --installcert -d ${CF_Domain} -d *.${CF_Domain} \
                    --fullchain-file ${certPath}/${CF_Domain}/fullchain.pem \
                    --key-file ${certPath}/${CF_Domain}/privkey.pem

                if [ $? -ne 0 ]; then
                    LOGE "Certificate installation failed, script exiting..."
                    exit 1
                else
                    LOGI "Certificate installed Successfully, Turning on automatic updates..."
                fi

                ~/.acme.sh/acme.sh --upgrade --auto-upgrade
                if [ $? -ne 0 ]; then
                    LOGE "Auto update setup failed, script exiting..."
                    exit 1
                else
                    LOGI "The certificate is installed and auto-renewal is turned on."
                    ls -lah ${certPath}/${CF_Domain}
                    chmod 755 ${certPath}/${CF_Domain}
                fi
            fi
            show_menu
            ;;
        3)
            echo "Exiting..."
            show_menu
            ;;
        *)
            echo "Invalid choice, please select again."
            show_menu
            ;;
    esac
}

generate_self_signed_cert() {
    cert_dir="/etc/sing-box"
    mkdir -p "$cert_dir"
    LOGI "Choose certificate type:"
    echo -e "${green}\t1.${plain} Ed25519 (*recommended*)"
    echo -e "${green}\t2.${plain} RSA 2048"
    echo -e "${green}\t3.${plain} RSA 4096"
    echo -e "${green}\t4.${plain} ECDSA prime256v1"
    echo -e "${green}\t5.${plain} ECDSA secp384r1"
    read -p "Enter your choice [1-5, default 1]: " cert_type
    cert_type=${cert_type:-1}

    case "$cert_type" in
        1)
            algo="ed25519"
            key_opt="-newkey ed25519"
            ;;
        2)
            algo="rsa"
            key_opt="-newkey rsa:2048"
            ;;
        3)
            algo="rsa"
            key_opt="-newkey rsa:4096"
            ;;
        4)
            algo="ecdsa"
            key_opt="-newkey ec -pkeyopt ec_paramgen_curve:prime256v1"
            ;;
        5)
            algo="ecdsa"
            key_opt="-newkey ec -pkeyopt ec_paramgen_curve:secp384r1"
            ;;
        *)
            algo="ed25519"
            key_opt="-newkey ed25519"
            ;;
    esac

    LOGI "Generating self-signed certificate ($algo)..."
    sudo openssl req -x509 -nodes -days 3650 $key_opt \
        -keyout "${cert_dir}/self.key" \
        -out "${cert_dir}/self.crt" \
        -subj "/CN=myserver"
    if [[ $? -eq 0 ]]; then
        sudo chmod 600 "${cert_dir}/self."*
        LOGI "Self-signed certificate generated successfully!"
        LOGI "Certificate path: ${cert_dir}/self.crt"
        LOGI "Key path: ${cert_dir}/self.key"
    else
        LOGE "Failed to generate self-signed certificate."
    fi
    before_show_menu
}

show_usage() {
    echo -e "2S-UI Control Menu Usage"
    echo -e "------------------------------------------"
    echo -e "SUBCOMMANDS:"
    echo -e "s-ui              - Admin Management Script"
    echo -e "s-ui start        - Start s-ui"
    echo -e "s-ui stop         - Stop s-ui"
    echo -e "s-ui restart      - Restart s-ui"
    echo -e "s-ui status       - Current Status of s-ui"
    echo -e "s-ui enable       - Enable Autostart on OS Startup"
    echo -e "s-ui disable      - Disable Autostart on OS Startup"
    echo -e "s-ui log          - Check s-ui Logs"
    echo -e "s-ui update       - Update"
    echo -e "s-ui install      - Install"
    echo -e "s-ui uninstall    - Uninstall"
    echo -e "s-ui help         - Control Menu Usage"
    echo -e "------------------------------------------"
}

switch_language() {
    # 语言列表固定用各语言原生名,不随当前语言翻译,谁都能找到自己的语言
    echo -e "$(t msg_lang_select)"
    echo -e "${green}\t1. English${plain}"
    echo -e "${green}\t2. 简体中文${plain}"
    echo -e "${green}\t3. 繁體中文${plain}"
    echo -e "${green}\t4. Русский${plain}"
    echo -e "${green}\t5. فارسی${plain}"
    echo -e "${green}\t6. Tiếng Việt${plain}"
    read -p "Select [1-6]: " lang_choice
    case "$lang_choice" in
    1) LANG_PREF="en" ;;
    2) LANG_PREF="zhcn" ;;
    3) LANG_PREF="zhtw" ;;
    4) LANG_PREF="ru" ;;
    5) LANG_PREF="fa" ;;
    6) LANG_PREF="vi" ;;
    *)
        echo "Invalid choice"
        show_menu
        return
        ;;
    esac
    echo "$LANG_PREF" > ~/.s2ui_lang
    LOGI "$(t msg_lang_saved)"
    show_menu
}

show_menu() {
  echo -e "
  ${green}$(t menu_title)${plain}
————————————————————————————————
  ${green}0.${plain} $(t opt_exit)
————————————————————————————————
  ${green}1.${plain} $(t opt_install)
  ${green}2.${plain} $(t opt_update)
  ${green}3.${plain} $(t opt_custom_ver)
  ${green}4.${plain} $(t opt_uninstall)
————————————————————————————————
  ${green}5.${plain} $(t opt_reset_admin)
  ${green}6.${plain} $(t opt_set_admin)
  ${green}7.${plain} $(t opt_view_admin)
————————————————————————————————
  ${green}8.${plain} $(t opt_reset_panel)
  ${green}9.${plain} $(t opt_set_panel)
  ${green}10.${plain} $(t opt_view_panel)
————————————————————————————————
  ${green}11.${plain} $(t opt_start)
  ${green}12.${plain} $(t opt_stop)
  ${green}13.${plain} $(t opt_restart)
  ${green}14.${plain} $(t opt_status)
  ${green}15.${plain} $(t opt_log)
  ${green}16.${plain} $(t opt_enable)
  ${green}17.${plain} $(t opt_disable)
————————————————————————————————
  ${green}18.${plain} $(t opt_bbr)
  ${green}19.${plain} $(t opt_ssl)
  ${green}20.${plain} $(t opt_cf_ssl)
————————————————————————————————
  ${green}21.${plain} $(t opt_lang)
————————————————————————————————
 "
    show_status s-ui
    echo && read -p "$(t prompt_select)" num

    case "${num}" in
    0)
        exit 0
        ;;
    1)
        check_uninstall && install
        ;;
    2)
        check_install && update
        ;;
    3)
        check_install && custom_version
        ;;
    4)
        check_install && uninstall
        ;;
    5)
        check_install && reset_admin
        ;;
    6)
        check_install && set_admin
        ;;
    7)
        check_install && view_admin
        ;;
    8)
        check_install && reset_setting
        ;;
    9)
        check_install && set_setting
        ;;
    10)
        check_install && view_setting
        ;;
    11)
        check_install && start s-ui
        ;;
    12)
        check_install && stop s-ui
        ;;
    13)
        check_install && restart s-ui
        ;;
    14)
        check_install && status s-ui
        ;;
    15)
        check_install && show_log s-ui
        ;;
    16)
        check_install && enable s-ui
        ;;
    17)
        check_install && disable s-ui
        ;;
    18)
        bbr_menu
        ;;
    19)
        ssl_cert_issue_main
        ;;
    20)
        ssl_cert_issue_CF
        ;;
    21)
        switch_language
        ;;
    *)
        LOGE "$(t err_invalid_num)"
        ;;
    esac
}

if [[ $# > 0 ]]; then
    case $1 in
    "start")
        check_install 0 && start s-ui 0
        ;;
    "stop")
        check_install 0 && stop s-ui 0
        ;;
    "restart")
        check_install 0 && restart s-ui 0
        ;;
    "status")
        check_install 0 && status 0
        ;;
    "enable")
        check_install 0 && enable s-ui 0
        ;;
    "disable")
        check_install 0 && disable s-ui 0
        ;;
    "log")
        check_install 0 && show_log s-ui 0
        ;;
    "update")
        check_install 0 && update 0
        ;;
    "install")
        check_uninstall 0 && install 0
        ;;
    "uninstall")
        check_install 0 && uninstall 0
        ;;
    *) show_usage ;;
    esac
else
    show_menu
fi
